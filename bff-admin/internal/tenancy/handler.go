// Package tenancy implements the admin BFF's tenant + merchant management over
// Core's TenantService/MerchantService. Platform-admin manages tenants;
// tenant-admin manages merchants under its own tenant. tenant_id is never in
// the path — it comes from the caller's scope (JWT/header seam), matching the
// PLAN's isolation rule.
package tenancy

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Handler holds the Core client.
type Handler struct {
	core *coreclient.Client
}

// New builds the tenancy handler.
func New(core *coreclient.Client) *Handler { return &Handler{core: core} }

// Routes mounts the tenant + merchant admin endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/admin/tenants", h.createTenant)
	r.Get("/admin/tenants", h.listTenants)
	r.Put("/admin/tenants/{tenantId}", h.updateTenant)

	r.Post("/admin/merchants", h.createMerchant)
	r.Get("/admin/merchants", h.listMerchants)
	r.Put("/admin/merchants/{merchantId}", h.updateMerchant)
}

type tenantBody struct {
	Name     string          `json:"name"`
	Plan     string          `json:"plan"`
	Settings json.RawMessage `json:"settings"`
}

func (h *Handler) createTenant(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	var body tenantBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Tenant.CreateTenant(ctx, &gamev1.CreateTenantRequest{
		Tenant: &gamev1.Tenant{Name: body.Name, Plan: body.Plan, Settings: string(orEmptyObj(body.Settings))},
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, tenantView(resp.GetTenant()))
}

func (h *Handler) listTenants(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Tenant.ListTenants(ctx, &gamev1.ListTenantsRequest{
		Limit: int32(parseLimit(r.URL.Query().Get("limit"))), Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetTenants()))
	for _, t := range resp.GetTenants() {
		items = append(items, tenantView(t))
	}
	envelope.WriteSuccess(w, tid, paged(items, resp.GetNextCursor()))
}

func (h *Handler) updateTenant(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	var body tenantBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Tenant.UpdateTenant(ctx, &gamev1.UpdateTenantRequest{
		Tenant: &gamev1.Tenant{
			Id: chi.URLParam(r, "tenantId"), Name: body.Name, Plan: body.Plan,
			Settings: string(orEmptyObj(body.Settings)),
		},
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, tenantView(resp.GetTenant()))
}

type merchantBody struct {
	Name     string          `json:"name"`
	Logo     string          `json:"logo"`
	Settings json.RawMessage `json:"settings"`
}

func (h *Handler) createMerchant(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, _ := auth.Scope(r) // tenant from the caller's scope, never the path
	var body merchantBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Merchant.CreateMerchant(ctx, &gamev1.CreateMerchantRequest{
		TenantId: tenant,
		Merchant: &gamev1.Merchant{Name: body.Name, Logo: body.Logo, Settings: string(orEmptyObj(body.Settings))},
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, merchantView(resp.GetMerchant()))
}

func (h *Handler) listMerchants(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, _ := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Merchant.ListMerchants(ctx, &gamev1.ListMerchantsRequest{
		TenantId: tenant, Limit: int32(parseLimit(r.URL.Query().Get("limit"))), Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetMerchants()))
	for _, m := range resp.GetMerchants() {
		items = append(items, merchantView(m))
	}
	envelope.WriteSuccess(w, tid, paged(items, resp.GetNextCursor()))
}

func (h *Handler) updateMerchant(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, _ := auth.Scope(r)
	var body merchantBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Merchant.UpdateMerchant(ctx, &gamev1.UpdateMerchantRequest{
		Merchant: &gamev1.Merchant{
			Id: chi.URLParam(r, "merchantId"), TenantId: tenant, Name: body.Name, Logo: body.Logo,
			Settings: string(orEmptyObj(body.Settings)),
		},
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, merchantView(resp.GetMerchant()))
}
