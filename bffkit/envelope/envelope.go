// Package envelope renders the uniform REST response contract
// {code, message, trace_id, data} to an http.ResponseWriter. The contract
// itself — the shape and the gRPC-status → error mapping — lives in the shared
// pkg/envelope so Core's grpc-gateway and any BFF render identical bytes. This
// package is the BFF-side http.ResponseWriter convenience layer.
package envelope

import (
	"encoding/json"
	"net/http"

	"github.com/muse/pkg/envelope"
)

// Envelope, ErrorInfo, and FieldViolation are re-exported from pkg/envelope so
// existing BFF code keeps referencing envelope.Envelope etc. unchanged.
type (
	Envelope       = envelope.Envelope
	ErrorInfo      = envelope.ErrorInfo
	FieldViolation = envelope.FieldViolation
)

// WriteSuccess writes a 200 envelope with the given data payload.
func WriteSuccess(w http.ResponseWriter, traceID string, data any) {
	write(w, http.StatusOK, envelope.Success(traceID, data))
}

// WriteCreated writes a 201 envelope.
func WriteCreated(w http.ResponseWriter, traceID string, data any) {
	write(w, http.StatusCreated, envelope.Success(traceID, data))
}

// WriteError maps any error (ideally a gRPC status from Core) to the error
// envelope with the canonical code, HTTP status, and structured ErrorInfo.
func WriteError(w http.ResponseWriter, traceID string, err error) {
	httpStatus, env := envelope.Error(traceID, err)
	write(w, httpStatus, env)
}

func write(w http.ResponseWriter, httpStatus int, env Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if env.TraceID != "" {
		w.Header().Set("X-Trace-Id", env.TraceID)
	}
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(env)
}
