// Package envelope renders the uniform REST response contract
// {code, message, trace_id, data} for success and error alike, and maps a gRPC
// status (from Core) into it. This presentation concern lives only in the BFF;
// Core/gamekit never know about it.
package envelope

import (
	"encoding/json"
	"net/http"

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

type errorData struct {
	Error ErrorInfo `json:"error"`
}

// WriteSuccess writes a 200 envelope with the given data payload.
func WriteSuccess(w http.ResponseWriter, traceID string, data any) {
	write(w, http.StatusOK, Envelope{Code: 0, Message: "ok", TraceID: traceID, Data: data})
}

// WriteCreated writes a 201 envelope.
func WriteCreated(w http.ResponseWriter, traceID string, data any) {
	write(w, http.StatusCreated, Envelope{Code: 0, Message: "ok", TraceID: traceID, Data: data})
}

// WriteError maps any error (ideally a gRPC status from Core) to the error
// envelope with the canonical code, HTTP status, and structured ErrorInfo.
func WriteError(w http.ResponseWriter, traceID string, err error) {
	st, ok := status.FromError(err)
	if !ok {
		st = status.New(codes.Internal, "internal error")
	}
	httpStatus := apierr.HTTPStatusForCode(st.Code())

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

	write(w, httpStatus, Envelope{
		Code:    int(st.Code()),
		Message: st.Message(),
		TraceID: traceID,
		Data:    errorData{Error: info},
	})
}

func write(w http.ResponseWriter, httpStatus int, env Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if env.TraceID != "" {
		w.Header().Set("X-Trace-Id", env.TraceID)
	}
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(env)
}
