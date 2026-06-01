// Package fulfillment implements the admin BFF's outbox surface: the staff task
// list + retry, and the signed machine callback the external orchestrator (n8n)
// calls to report a delivery result. The callback is HMAC-verified at this edge
// before it is forwarded to Core, so Core trusts the result by task id.
package fulfillment

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Handler serves the admin fulfillment endpoints + the n8n callback.
type Handler struct {
	core           *coreclient.Client
	callbackSecret string // shared HMAC secret for inbound callbacks; "" = dev (unverified)
	log            *slog.Logger
}

// New builds the handler. callbackSecret is the shared secret used to verify the
// X-Muse-Signature on inbound callbacks; empty means dev mode (signature not
// enforced, logged loudly).
func New(core *coreclient.Client, callbackSecret string, log *slog.Logger) *Handler {
	return &Handler{core: core, callbackSecret: callbackSecret, log: log}
}

// Routes mounts the admin task management + the (non-admin, machine) callback.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/admin/fulfillment/tasks", h.listTasks)
	r.Post("/admin/fulfillment/tasks/{taskId}/retry", h.retryTask)
	// The callback is called by the orchestrator (n8n), not a logged-in admin;
	// it is authenticated by HMAC, not by JWT/role.
	r.Post("/fulfillment/tasks/{taskId}/callback", h.callback)
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	q := r.URL.Query()
	resp, err := h.core.Fulfillment.ListTasks(ctx, &gamev1.ListTasksRequest{
		TenantId:      tenant, MerchantId: merchant,
		Status:     enumx.Parse[gamev1.TaskStatus](q.Get("status"), gamev1.TaskStatus_value),
		CampaignId: q.Get("campaign_id"),
		PrizeId:    q.Get("prize_id"),
		Limit:      int32(parseLimit(q.Get("limit"))),
		Cursor:     q.Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetTasks()))
	for _, t := range resp.GetTasks() {
		items = append(items, taskView(t))
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"items": items,
		"pagination": map[string]any{
			"next_cursor": emptyToNil(resp.GetNextCursor()),
			"has_more":    resp.GetNextCursor() != "",
		},
	})
}

func (h *Handler) retryTask(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Fulfillment.RetryTask(ctx, &gamev1.RetryTaskRequest{
		TenantId: tenant, MerchantId: merchant, TaskId: chi.URLParam(r, "taskId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, taskView(resp.GetTask()))
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		envelope.WriteError(w, tid, invalidArg("unreadable body"))
		return
	}
	if !h.verifySignature(r, body) {
		envelope.WriteError(w, tid, unauthenticated("invalid or missing callback signature"))
		return
	}
	var in struct {
		Status  string          `json:"status"`
		Receipt json.RawMessage `json:"receipt"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed callback body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Fulfillment.ReportResult(ctx, &gamev1.ReportResultRequest{
		TaskId:  chi.URLParam(r, "taskId"),
		Status:  enumx.Parse[gamev1.TaskStatus](in.Status, gamev1.TaskStatus_value),
		Receipt: string(in.Receipt),
		Error:   in.Error,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, taskView(resp.GetTask()))
}

// verifySignature checks the X-Muse-Signature HMAC over the raw body. With no
// configured secret it runs in dev mode (accepts, logs a warning) so local e2e
// works without key material.
func (h *Handler) verifySignature(r *http.Request, body []byte) bool {
	if h.callbackSecret == "" {
		if h.log != nil {
			h.log.Warn("fulfillment callback accepted WITHOUT signature verification (no secret configured)")
		}
		return true
	}
	got := r.Header.Get("X-Muse-Signature")
	if got == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.callbackSecret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func taskView(t *gamev1.FulfillmentTask) map[string]any {
	return map[string]any{
		"task_id":         t.GetId(),
		"reward_id":       emptyToNil(t.GetRewardId()),
		"prize_id":        emptyToNil(t.GetPrizeId()),
		"player_id":       emptyToNil(t.GetPlayerId()),
		"game_id":         emptyToNil(t.GetGameId()),
		"campaign_id":     emptyToNil(t.GetCampaignId()),
		"channel":         t.GetChannel(),
		"status":          enumx.Name(t.GetStatus()),
		"attempts":        t.GetAttempts(),
		"max_attempts":    t.GetMaxAttempts(),
		"last_error":      emptyToNil(t.GetLastError()),
		"receipt":         rawJSON(t.GetReceipt()),
		"next_attempt_at": tsString(t.GetNextAttemptAt()),
		"created_at":      tsString(t.GetCreatedAt()),
		"updated_at":      tsString(t.GetUpdatedAt()),
	}
}
