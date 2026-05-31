// Package fulfillment is Core's delivery layer over the transactional outbox:
// a FulfillmentProvider registry (one impl per channel, same pluggable pattern
// as the gamekit reward handlers) plus a dispatcher worker that drains pending
// tasks and invokes the configured provider with retry/backoff and a
// dead-letter terminal state. This is hosting-layer (I/O) code, so it lives in
// Core, not in the pure gamekit SDK.
package fulfillment

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit/types"
)

// Outcome is the result class of a single delivery attempt.
type Outcome int

const (
	// Delivered: the channel completed delivery synchronously (terminal success).
	Delivered Outcome = iota
	// AwaitingCallback: handed off to an external orchestrator (n8n); the task
	// stays processing until its signed callback finalizes it.
	AwaitingCallback
	// RetryableError: a transient failure; the dispatcher reschedules with
	// backoff until the attempt budget is exhausted (then dead-letter).
	RetryableError
	// PermanentError: an unrecoverable failure (e.g. misconfiguration); the task
	// goes straight to dead-letter without consuming further retries.
	PermanentError
)

// Result is what a Provider returns for one Deliver call.
type Result struct {
	Outcome Outcome
	Receipt json.RawMessage // provider proof (voucher code, message id, ...), stored on the task
	Err     error           // populated for RetryableError / PermanentError
}

// Delivered builds a terminal-success result with an optional receipt.
func DeliveredResult(receipt json.RawMessage) Result {
	return Result{Outcome: Delivered, Receipt: receipt}
}

// Awaiting builds an awaiting-callback result (external_workflow).
func Awaiting(receipt json.RawMessage) Result {
	return Result{Outcome: AwaitingCallback, Receipt: receipt}
}

// Retry builds a transient-failure result.
func Retry(err error) Result { return Result{Outcome: RetryableError, Err: err} }

// Permanent builds an unrecoverable-failure result.
func Permanent(err error) Result { return Result{Outcome: PermanentError, Err: err} }

// Provider delivers a task over one channel. Implementations must be safe to
// call concurrently and idempotent on the task id (deliveries can be retried).
type Provider interface {
	Deliver(ctx context.Context, task *types.FulfillmentTask) Result
}

// Registry maps a channel name to its Provider. Adding a channel = register one
// impl; the dispatcher and engine never change.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{providers: map[string]Provider{}} }

// Register binds a provider to a channel (last registration wins).
func (r *Registry) Register(channel string, p Provider) { r.providers[channel] = p }

// Get returns the provider for a channel, or (nil, false) if none is registered.
func (r *Registry) Get(channel string) (Provider, bool) {
	p, ok := r.providers[channel]
	return p, ok
}
