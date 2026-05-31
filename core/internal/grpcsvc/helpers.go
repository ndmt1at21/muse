package grpcsvc

import (
	"context"
	"strings"
	"time"

	"github.com/muse/gamekit/defaults"
	"github.com/muse/gamekit/gkerr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var idgen defaults.IDGen

// randSuffix mints the opaque tail of an entity ID.
func randSuffix() string {
	// NewID returns "<prefix>_<hex>"; we only want the hex tail here.
	full := idgen.NewID("x")
	return full[len("x_"):]
}

// notOwned builds a PERMISSION_DENIED error for cross-player reward access.
func notOwned(rewardID string) error {
	return gkerr.New(gkerr.ReasonPermissionDenied, "reward does not belong to caller").
		WithMeta("reward_id", rewardID)
}

type traceIDKey struct{}

// traceIDFrom returns the W3C-ish trace id propagated from the BFF (via the
// "x-trace-id" gRPC metadata), or "" if absent. The BFF sets this from the
// OTel trace id it puts in the response envelope, so a client error links to
// its server trace.
func traceIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-trace-id"); len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

// TraceIDInterceptor stashes the inbound x-trace-id into the context so the
// engine can stamp it onto immutable history. Returned for wiring in main.
func TraceIDInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-trace-id"); len(vals) > 0 {
			ctx = context.WithValue(ctx, traceIDKey{}, vals[0])
		}
	}
	return handler(ctx, req)
}

// GRPCObserver is the metrics sink the RED interceptor reports to (satisfied by
// *platform.Metrics). Kept as a local interface so grpcsvc doesn't import platform.
type GRPCObserver interface {
	ObserveGRPC(method, code string, dur time.Duration)
}

// MetricsInterceptor records RED metrics (rate, errors, duration) per RPC. The
// method label is the short RPC name; the code label is the canonical gRPC
// status. A nil observer makes this a pass-through.
func MetricsInterceptor(obs GRPCObserver) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if obs == nil {
			return handler(ctx, req)
		}
		start := time.Now()
		resp, err := handler(ctx, req)
		obs.ObserveGRPC(shortMethod(info.FullMethod), status.Code(err).String(), time.Since(start))
		return resp, err
	}
}

// shortMethod trims "/game.v1.EngineService/Play" to "Play".
func shortMethod(full string) string {
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[i+1:]
	}
	return full
}
