package middleware

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// statusInternal builds a generic INTERNAL gRPC status for panic recovery so
// the envelope renderer produces a consistent 500 error body.
func statusInternal() error {
	return status.New(codes.Internal, "internal error").Err()
}
