// Package envelope is the uniform REST response contract shared by Core's
// grpc-gateway and any BFF a developer builds (bffkit re-exports it). Every
// response — success or error — is {code, message, trace_id, data}. A gRPC
// status from Core maps deterministically into the error shape here. This is a
// pure presentation layer: gamekit/Core's engine never knows about it.
package envelope

import (
	"github.com/muse/pkg/apierr"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Envelope is the top-level shape of every response.
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
	Data    any    `json:"data"`
}

// ErrorInfo mirrors google.rpc.ErrorInfo for the data.error payload.
type ErrorInfo struct {
	Status          string            `json:"status"`
	Reason          string            `json:"reason"`
	Domain          string            `json:"domain"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	FieldViolations []FieldViolation  `json:"field_violations,omitempty"`
}

// FieldViolation mirrors google.rpc.BadRequest.FieldViolation.
type FieldViolation struct {
	Field       string `json:"field"`
	Description string `json:"description"`
}

// ErrorData is the data payload for an error envelope.
type ErrorData struct {
	Error ErrorInfo `json:"error"`
}

// Success builds a success envelope (code 0, message "ok") around data.
func Success(traceID string, data any) Envelope {
	return Envelope{Code: 0, Message: "ok", TraceID: traceID, Data: data}
}

// Error maps any error (ideally a gRPC status from Core) into the HTTP status
// and the error envelope with the canonical code, message, and structured
// ErrorInfo. It is pure: callers decide how to write it (http.ResponseWriter,
// a gateway error handler, …).
func Error(traceID string, err error) (httpStatus int, env Envelope) {
	st, ok := status.FromError(err)
	if !ok {
		st = status.New(codes.Internal, "internal error")
	}
	httpStatus = apierr.HTTPStatusForCode(st.Code())

	info := ErrorInfo{Status: st.Code().String(), Domain: apierr.Domain, Reason: "INTERNAL"}
	for _, d := range st.Details() {
		switch v := d.(type) {
		case *errdetails.ErrorInfo:
			info.Reason = v.GetReason()
			if v.GetDomain() != "" {
				info.Domain = v.GetDomain()
			}
			info.Metadata = v.GetMetadata()
		case *errdetails.BadRequest:
			for _, fv := range v.GetFieldViolations() {
				info.FieldViolations = append(info.FieldViolations, FieldViolation{
					Field: fv.GetField(), Description: fv.GetDescription(),
				})
			}
		}
	}
	if st.Code() == codes.Internal {
		info.Reason = "INTERNAL"
	}

	return httpStatus, Envelope{
		Code:    int(st.Code()),
		Message: st.Message(),
		TraceID: traceID,
		Data:    ErrorData{Error: info},
	}
}
