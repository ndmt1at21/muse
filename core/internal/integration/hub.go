package integration

import (
	"context"
	"log/slog"

	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
)

// Store is the persistence the hub needs (satisfied by *sqlstore.DB).
type Store interface {
	CreateIntegration(ctx context.Context, i *types.Integration) (*types.Integration, error)
	DeleteIntegration(ctx context.Context, tenantID, merchantID, id string) error
	ListIntegrations(ctx context.Context, tenantID, merchantID, campaignID string, limit int, cursor string) ([]types.Integration, string, error)
	ListIntegrationsForEvent(ctx context.Context, scope types.Scope, campaignID, eventType string) ([]types.Integration, error)
}

// Bus is the optional pub/sub fan-out the hub publishes every event to (Redis in
// deploy). nil disables fan-out; in-process integration dispatch still runs.
type Bus interface {
	Publish(ctx context.Context, evt ports.Event) error
}

// Hub is the integration dispatcher and the engine's EventSink. On each emitted
// event it (1) publishes to the bus for cross-process fan-out, and (2) looks up
// the integrations subscribed to that event in the event's scope and delivers it
// through their providers. Everything is best-effort and must never fail the
// originating domain operation — Emit returns nothing and only logs failures.
type Hub struct {
	store Store
	reg   *Registry
	bus   Bus
	log   *slog.Logger
}

// NewHub builds the hub. bus may be nil (no fan-out); store/reg are required.
func NewHub(store Store, reg *Registry, bus Bus, log *slog.Logger) *Hub {
	return &Hub{store: store, reg: reg, bus: bus, log: log}
}

// Emit implements ports.EventSink. It is called best-effort post-commit; it runs
// dispatch synchronously but swallows all errors (logging them), so a slow or
// failing integration never blocks correctness — callers already committed.
func (h *Hub) Emit(ctx context.Context, evt ports.Event) {
	if h.bus != nil {
		if err := h.bus.Publish(ctx, evt); err != nil && h.log != nil {
			h.log.Warn("event bus publish failed", "event", evt.Type, "err", err)
		}
	}
	h.dispatch(ctx, evt)
}

// Dispatch delivers an event to every matching integration and reports how many
// were attempted (used by the EmitEvent RPC). campaignID is read from the event
// payload ("campaign_id") when present to honor campaign-narrowed integrations.
func (h *Hub) Dispatch(ctx context.Context, evt ports.Event) int {
	return h.dispatch(ctx, evt)
}

func (h *Hub) dispatch(ctx context.Context, evt ports.Event) int {
	campaignID, _ := evt.Payload["campaign_id"].(string)
	integs, err := h.store.ListIntegrationsForEvent(ctx, evt.Scope, campaignID, evt.Type)
	if err != nil {
		if h.log != nil {
			h.log.Warn("integration lookup failed", "event", evt.Type, "err", err)
		}
		return 0
	}
	dispatched := 0
	for i := range integs {
		integ := integs[i]
		provider, ok := h.reg.Get(integ.Type)
		if !ok {
			if h.log != nil {
				h.log.Warn("no provider for integration type", "type", integ.Type, "integration_id", integ.ID)
			}
			continue
		}
		if err := provider.Deliver(ctx, integ, evt); err != nil {
			if h.log != nil {
				h.log.Warn("integration delivery failed", "type", integ.Type,
					"integration_id", integ.ID, "event", evt.Type, "err", err)
			}
			continue
		}
		dispatched++
	}
	return dispatched
}

// --- CRUD service surface (backs IntegrationService) ---

// Create stores a new integration.
func (h *Hub) Create(ctx context.Context, i *types.Integration) (*types.Integration, error) {
	return h.store.CreateIntegration(ctx, i)
}

// Delete removes an integration.
func (h *Hub) Delete(ctx context.Context, tenantID, merchantID, id string) error {
	return h.store.DeleteIntegration(ctx, tenantID, merchantID, id)
}

// List returns integrations under the merchant (optionally one campaign).
func (h *Hub) List(ctx context.Context, tenantID, merchantID, campaignID string, limit int, cursor string) ([]types.Integration, string, error) {
	return h.store.ListIntegrations(ctx, tenantID, merchantID, campaignID, limit, cursor)
}
