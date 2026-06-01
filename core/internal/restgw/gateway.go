// Package restgw is Core's REST surface: a grpc-gateway that translates
// JSON/HTTP (the google.api.http annotations in game.v1) to Core's own gRPC and
// wraps every response in the uniform {code, message, trace_id, data} envelope.
//
// Core is auth-agnostic. The gateway verifies no token and enforces no role —
// the tenant/merchant Scope arrives as ordinary request fields, and a BFF the
// developer builds (see examples/) is responsible for authenticating callers
// and filling that Scope. The only cross-cutting concern here is trace-id
// propagation, so a REST call correlates with the gRPC work and history rows it
// triggers.
package restgw

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/muse/pkg/envelope"
	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	traceHeader = "X-Trace-Id"
	traceMeta   = "x-trace-id"
)

// New builds the REST gateway handler. It dials Core's own gRPC at grpcAddr
// (loopback over the local network) and registers every game.v1 service. The
// returned closer releases the gateway's gRPC connection.
func New(ctx context.Context, grpcAddr string) (http.Handler, func() error, error) {
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}

	// snake_case field names + zero values, so the JSON surface mirrors the proto
	// contract predictably for hand-written clients.
	jsonpb := &runtime.JSONPb{
		MarshalOptions:   protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true},
		UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: true},
	}

	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, jsonpb),
		runtime.WithMetadata(traceAnnotator),
		runtime.WithForwardResponseRewriter(successRewriter(jsonpb)),
		runtime.WithErrorHandler(errorHandler),
	)

	register := []func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error{
		gamev1.RegisterEngineServiceHandler,
		gamev1.RegisterGameConfigServiceHandler,
		gamev1.RegisterRewardServiceHandler,
		gamev1.RegisterFulfillmentServiceHandler,
		gamev1.RegisterTenantServiceHandler,
		gamev1.RegisterMerchantServiceHandler,
		gamev1.RegisterIdentityServiceHandler,
		gamev1.RegisterPlayerServiceHandler,
		gamev1.RegisterCampaignServiceHandler,
		gamev1.RegisterQuestServiceHandler,
		gamev1.RegisterLeaderboardServiceHandler,
		gamev1.RegisterWalletServiceHandler,
		gamev1.RegisterIntegrationServiceHandler,
	}
	for _, reg := range register {
		if err := reg(ctx, mux, conn); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}

	return withTraceID(mux), conn.Close, nil
}

// successRewriter wraps a successful response in the success envelope. The proto
// payload is pre-marshaled with the gateway's JSONPb options and embedded as
// raw JSON so the outer struct marshal preserves proto field semantics.
func successRewriter(m *runtime.JSONPb) func(context.Context, proto.Message) (any, error) {
	return func(ctx context.Context, resp proto.Message) (any, error) {
		data, err := m.Marshal(resp)
		if err != nil {
			return nil, err
		}
		data = numberizeTimestamps(resp, data)
		return envelope.Success(traceFromCtx(ctx), json.RawMessage(data)), nil
	}
}

// numberizeTimestamps rewrites protojson output so unix-timestamp fields — which
// the proto3 JSON spec forces protojson to encode as quoted strings (e.g.
// "created_at": "1717200000") — are emitted as JSON numbers instead
// ("created_at": 1717200000). Only timestamp fields are touched; other int64
// values (counts, balances, scores) stay as strings, which is fine. The walk is
// guided by proto reflection, so enums (rendered as names), string ids, and
// google.protobuf.Struct payloads are left untouched. Returns data unchanged on
// any decode/encode error.
func numberizeTimestamps(resp proto.Message, data []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return data
	}
	tree = fixMessage(resp.ProtoReflect().Descriptor(), tree)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // preserve <, >, & inside raw-JSON (Struct) payloads
	if err := enc.Encode(tree); err != nil {
		return data
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// fixMessage walks the decoded JSON object of a message, converting timestamp
// leaves to numbers and recursing into nested/repeated messages.
func fixMessage(md protoreflect.MessageDescriptor, node any) any {
	obj, ok := node.(map[string]any)
	if !ok {
		return node
	}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		key := string(fd.Name()) // gateway marshals with UseProtoNames
		v, present := obj[key]
		if !present {
			continue
		}
		switch {
		case isTimestampField(fd):
			obj[key] = toNumber(v)
		case fd.Kind() == protoreflect.MessageKind && !isWellKnown(fd.Message()):
			if fd.IsList() {
				if arr, ok := v.([]any); ok {
					for i, e := range arr {
						arr[i] = fixMessage(fd.Message(), e)
					}
				}
			} else if !fd.IsMap() {
				obj[key] = fixMessage(fd.Message(), v)
			}
		}
	}
	return obj
}

// isTimestampField reports whether fd is one of Core's int64 unix-timestamp
// fields (created_at, updated_at, *_at, *_date, last_completed). These are all
// plain int64 scalars; counts/balances/scores share the type but are not times.
func isTimestampField(fd protoreflect.FieldDescriptor) bool {
	if fd.Kind() != protoreflect.Int64Kind || fd.IsList() || fd.IsMap() {
		return false
	}
	n := string(fd.Name())
	return strings.HasSuffix(n, "_at") || strings.HasSuffix(n, "_date") || n == "last_completed"
}

// toNumber turns protojson's quoted int64 string into an unquoted JSON number.
func toNumber(v any) any {
	if s, ok := v.(string); ok {
		return json.Number(s)
	}
	return v
}

// isWellKnown reports whether md is a google.protobuf.* type (Struct, Value, …),
// whose JSON shape does not follow its field descriptors and must not be walked.
func isWellKnown(md protoreflect.MessageDescriptor) bool {
	return strings.HasPrefix(string(md.FullName()), "google.protobuf.")
}

// errorHandler renders any gRPC status from Core as the error envelope.
func errorHandler(ctx context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, _ *http.Request, err error) {
	tid := traceFromCtx(ctx)
	httpStatus, env := envelope.Error(tid, err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if tid != "" {
		w.Header().Set(traceHeader, tid)
	}
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(env)
}

// traceAnnotator lifts the request's trace id into outgoing gRPC metadata, so
// Core stamps it onto immutable history and the envelope echoes it.
func traceAnnotator(_ context.Context, r *http.Request) metadata.MD {
	if tid := r.Header.Get(traceHeader); tid != "" {
		return metadata.Pairs(traceMeta, tid)
	}
	return nil
}

func traceFromCtx(ctx context.Context) string {
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if v := md.Get(traceMeta); len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// withTraceID honors an inbound X-Trace-Id or generates one, exposes it on the
// response header, and stashes it on the request so the annotator forwards a
// consistent id to gRPC.
func withTraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := r.Header.Get(traceHeader)
		if tid == "" {
			tid = newTraceID()
			r.Header.Set(traceHeader, tid)
		}
		w.Header().Set(traceHeader, tid)
		next.ServeHTTP(w, r)
	})
}

func newTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
