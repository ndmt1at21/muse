// Package integration implements the admin BFF's integration management over
// Core's IntegrationService: register/list/delete outbound adapters (webhook,
// gsheet, sms, zns, crm, n8n) and a manual EmitEvent for testing a wiring.
// tenant_id + merchant_id come from the caller's scope (JWT/header seam).
package integration

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Handler holds the Core client.
type Handler struct {
	core *coreclient.Client
}

// New builds the integration admin handler.
func New(core *coreclient.Client) *Handler { return &Handler{core: core} }

// Routes mounts the integration admin endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/admin/integrations", h.create)
	r.Get("/admin/integrations", h.list)
	r.Delete("/admin/integrations/{integrationId}", h.delete)
	r.Post("/admin/integrations/emit", h.emit) // manual event injection (testing)
}

type integrationBody struct {
	Type       string          `json:"type"`
	CampaignID string          `json:"campaign_id"`
	Events     []string        `json:"events"`
	Config     json.RawMessage `json:"config"`
	Status     string          `json:"status"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body integrationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Integration.CreateIntegration(ctx, &gamev1.CreateIntegrationRequest{
		TenantId: tenant, MerchantId: merchant,
		Integration: &gamev1.Integration{
			Type:       enumx.Parse[gamev1.IntegrationType](body.Type, gamev1.IntegrationType_value),
			CampaignId: body.CampaignID,
			Events:     body.Events,
			Config:     string(orEmptyObj(body.Config)),
			Status:     enumx.Parse[gamev1.IntegrationStatus](body.Status, gamev1.IntegrationStatus_value),
		},
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, integrationView(resp.GetIntegration()))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Integration.ListIntegrations(ctx, &gamev1.ListIntegrationsRequest{
		TenantId: tenant, MerchantId: merchant,
		CampaignId: r.URL.Query().Get("campaignId"),
		Limit:      int32(parseLimit(r.URL.Query().Get("limit"))),
		Cursor:     r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetIntegrations()))
	for _, i := range resp.GetIntegrations() {
		items = append(items, integrationView(i))
	}
	envelope.WriteSuccess(w, tid, paged(items, resp.GetNextCursor()))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	_, err := h.core.Integration.DeleteIntegration(ctx, &gamev1.DeleteIntegrationRequest{
		TenantId: tenant, MerchantId: merchant,
		Id: chi.URLParam(r, "integrationId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{"deleted": true})
}

func (h *Handler) emit(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Integration.EmitEvent(ctx, &gamev1.EmitEventRequest{
		TenantId: tenant, MerchantId: merchant,
		Type:    body.Type,
		Payload: string(body.Payload),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{"dispatched": resp.GetDispatched()})
}

// --- helpers (package-local; mirrors the other admin handlers) ---

func integrationView(i *gamev1.Integration) map[string]any {
	if i == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":          i.GetId(),
		"type":        enumx.Name(i.GetType()),
		"campaign_id": emptyToNil(i.GetCampaignId()),
		"events":      i.GetEvents(),
		"config":      rawJSON(i.GetConfig()),
		"status":      enumx.Name(i.GetStatus()),
		"created_at":  tsString(i.GetCreatedAt()),
	}
}

func invalidArg(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonValidationFailed, msg)).Err()
}

func orEmptyObj(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return b
}

func paged(items []any, nextCursor string) map[string]any {
	return map[string]any{
		"items":      items,
		"pagination": map[string]any{"next_cursor": emptyToNil(nextCursor), "has_more": nextCursor != ""},
	}
}

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func rawJSON(s string) any {
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return v
}

// tsString returns the unix-seconds timestamp, or nil when unset (0).
func tsString(unix int64) any {
	if unix == 0 {
		return nil
	}
	return unix
}

func parseLimit(s string) int {
	if s == "" {
		return 20
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}
