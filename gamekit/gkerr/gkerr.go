// Package gkerr defines the engine's domain errors. They carry a stable,
// machine-readable Reason (UPPER_SNAKE_CASE) that the hosting layer (Core)
// maps to a canonical gRPC/HTTP status. The SDK itself knows nothing about
// gRPC codes or HTTP — it only speaks in business reasons.
package gkerr

import (
	"errors"
	"fmt"
)

// Error is a domain error with a stable reason and optional metadata.
type Error struct {
	Reason   string         // stable machine string, e.g. PRIZE_OUT_OF_STOCK
	Message  string         // human-readable, actionable
	Metadata map[string]any // structured context (prize_id, game_id, ...)
	Err      error          // wrapped cause, if any
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Reason, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Reason, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// New builds a domain error.
func New(reason, message string) *Error {
	return &Error{Reason: reason, Message: message}
}

// Newf builds a domain error with a formatted message.
func Newf(reason, format string, args ...any) *Error {
	return &Error{Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// WithMeta attaches metadata and returns the same error (chainable).
func (e *Error) WithMeta(k string, v any) *Error {
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	e.Metadata[k] = v
	return e
}

// Wrap attaches an underlying cause.
func (e *Error) Wrap(err error) *Error { e.Err = err; return e }

// ReasonOf extracts the stable reason from any error, or "" if not a domain error.
func ReasonOf(err error) string {
	var de *Error
	if errors.As(err, &de) {
		return de.Reason
	}
	return ""
}

// As reports whether err is a *Error and returns it.
func As(err error) (*Error, bool) {
	var de *Error
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// Canonical reasons used by the engine (see PLAN Error Catalog).
const (
	ReasonValidationFailed  = "VALIDATION_FAILED"
	ReasonUnauthenticated   = "UNAUTHENTICATED"
	ReasonNotFound          = "RESOURCE_NOT_FOUND"
	ReasonSessionExpired    = "SESSION_EXPIRED"
	ReasonSessionConsumed   = "SESSION_CONSUMED"
	ReasonSessionInvalid    = "SESSION_INVALID"
	ReasonOutOfTurns        = "OUT_OF_TURNS"
	ReasonPrizeOutOfStock   = "PRIZE_OUT_OF_STOCK"
	ReasonGameNotActive     = "GAME_NOT_ACTIVE"
	ReasonCheatDetected     = "CHEAT_DETECTED"
	ReasonInternal          = "INTERNAL"
	ReasonHandlerNotFound   = "HANDLER_NOT_FOUND"
	ReasonRewardBadState    = "REWARD_INVALID_STATE"
	ReasonRewardAlreadyDone = "REWARD_ALREADY_CLAIMED"
	ReasonPermissionDenied  = "PERMISSION_DENIED"
	ReasonTaskBadState      = "TASK_INVALID_STATE"
	ReasonContactConflict   = "CONTACT_CONFLICT"
	ReasonAlreadyExists     = "ALREADY_EXISTS"
	ReasonRateLimited       = "RATE_LIMITED"
)
