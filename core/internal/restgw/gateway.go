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
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/muse/pkg/envelope"
	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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
		gamev1.RegisterGameAdminServiceHandler,
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
		return envelope.Success(traceFromCtx(ctx), json.RawMessage(data)), nil
	}
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
