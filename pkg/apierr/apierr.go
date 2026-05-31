// Package apierr bridges the engine's domain errors (gamekit/gkerr) to the
// canonical gRPC/HTTP error model from the PLAN's Error Catalog. Core converts
// a gkerr.Error into a gRPC status carrying an errdetails.ErrorInfo (stable
// reason + domain + metadata); the BFF reads it back and renders the uniform
// {code, message, trace_id, data} envelope.
package apierr

import (
	"fmt"
	"net/http"

	"github.com/muse/gamekit/gkerr"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Domain is the error domain stamped on every ErrorInfo.
const Domain = "muse.game"

// reasonCode maps a stable gkerr reason to its canonical gRPC code.
// Reasons not listed default to codes.Internal.
var reasonCode = map[string]codes.Code{
	gkerr.ReasonValidationFailed:  codes.InvalidArgument,
	gkerr.ReasonCheatDetected:     codes.InvalidArgument,
	gkerr.ReasonUnauthenticated:   codes.Unauthenticated,
	gkerr.ReasonNotFound:          codes.NotFound,
	gkerr.ReasonSessionExpired:    codes.FailedPrecondition,
	gkerr.ReasonSessionConsumed:   codes.FailedPrecondition,
	gkerr.ReasonSessionInvalid:    codes.FailedPrecondition,
	gkerr.ReasonOutOfTurns:        codes.FailedPrecondition,
	gkerr.ReasonGameNotActive:     codes.FailedPrecondition,
	gkerr.ReasonPrizeOutOfStock:   codes.Aborted,
	gkerr.ReasonHandlerNotFound:   codes.Internal,
	gkerr.ReasonInternal:          codes.Internal,
	gkerr.ReasonRewardBadState:    codes.FailedPrecondition,
	gkerr.ReasonRewardAlreadyDone: codes.AlreadyExists,
	gkerr.ReasonPermissionDenied:  codes.PermissionDenied,
	gkerr.ReasonTaskBadState:      codes.FailedPrecondition,
	gkerr.ReasonContactConflict:   codes.AlreadyExists,
	gkerr.ReasonAlreadyExists:     codes.AlreadyExists,
	gkerr.ReasonRateLimited:       codes.ResourceExhausted,
}

// CodeForReason returns the canonical gRPC code for a stable reason.
func CodeForReason(reason string) codes.Code {
	if c, ok := reasonCode[reason]; ok {
		return c
	}
	return codes.Internal
}

// httpForCode maps a gRPC code to the HTTP status per Google's mapping.
var httpForCode = map[codes.Code]int{
	codes.OK:                 http.StatusOK,
	codes.InvalidArgument:    http.StatusBadRequest,
	codes.FailedPrecondition: http.StatusBadRequest,
	codes.Unauthenticated:    http.StatusUnauthorized,
	codes.PermissionDenied:   http.StatusForbidden,
	codes.NotFound:           http.StatusNotFound,
	codes.AlreadyExists:      http.StatusConflict,
	codes.Aborted:            http.StatusConflict,
	codes.ResourceExhausted:  http.StatusTooManyRequests,
	codes.Unimplemented:      http.StatusNotImplemented,
	codes.Unavailable:        http.StatusServiceUnavailable,
	codes.DeadlineExceeded:   http.StatusGatewayTimeout,
	codes.Internal:           http.StatusInternalServerError,
}

// HTTPStatusForCode returns the HTTP status for a gRPC code (default 500).
func HTTPStatusForCode(c codes.Code) int {
	if s, ok := httpForCode[c]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// FromDomainError converts any error into a gRPC *status.Status. If it is a
// gkerr.Error its reason/metadata become an ErrorInfo detail; otherwise it
// becomes a generic INTERNAL status (the original message is not leaked).
func FromDomainError(err error) *status.Status {
	if err == nil {
		return status.New(codes.OK, "ok")
	}
	de, ok := gkerr.As(err)
	if !ok {
		return status.New(codes.Internal, "internal error")
	}
	st := status.New(CodeForReason(de.Reason), de.Message)
	info := &errdetails.ErrorInfo{
		Reason:   de.Reason,
		Domain:   Domain,
		Metadata: stringMeta(de.Metadata),
	}
	if withDetails, derr := st.WithDetails(info); derr == nil {
		return withDetails
	}
	return st
}

func stringMeta(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = toString(v)
	}
	return out
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}
