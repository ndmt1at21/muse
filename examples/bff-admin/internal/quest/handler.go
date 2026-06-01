// Package quest implements the admin BFF's quest management over Core's
// QuestService. Quests live under a campaign; tenant_id + merchant_id come from
// the caller's scope (JWT/header seam), never the path.
package quest

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Handler holds the Core client.
type Handler struct {
	core *coreclient.Client
}

// New builds the quest admin handler.
func New(core *coreclient.Client) *Handler { return &Handler{core: core} }

// Routes mounts the quest admin endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/admin/quests", h.createQuest)
	r.Get("/admin/quests", h.listQuests)
	r.Put("/admin/quests/{questId}", h.updateQuest)
	r.Delete("/admin/quests/{questId}", h.deleteQuest)
}

type questReward struct {
	Type     string `json:"type"`
	Quantity int64  `json:"quantity"`
}

type questBody struct {
	CampaignID string          `json:"campaign_id"`
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	Reward     questReward     `json:"reward"`
	Config     json.RawMessage `json:"config"`
}

func (b questBody) toProto(id string) *gamev1.Quest {
	return &gamev1.Quest{
		Id:         id,
		CampaignId: b.CampaignID,
		Type:       enumx.Parse[gamev1.QuestType](b.Type, gamev1.QuestType_value),
		Name:       b.Name,
		Status:     enumx.Parse[gamev1.QuestStatus](b.Status, gamev1.QuestStatus_value),
		Reward:     &gamev1.QuestReward{Type: enumx.Parse[gamev1.QuestRewardType](b.Reward.Type, gamev1.QuestRewardType_value), Quantity: b.Reward.Quantity},
		Config:     string(orEmptyObj(b.Config)),
	}
}

func (h *Handler) createQuest(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body questBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Quest.CreateQuest(ctx, &gamev1.CreateQuestRequest{
		TenantId: tenant, MerchantId: merchant,
		Quest: body.toProto(""),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, questView(resp.GetQuest()))
}

func (h *Handler) listQuests(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Quest.ListQuests(ctx, &gamev1.ListQuestsRequest{
		TenantId:      tenant, MerchantId: merchant,
		CampaignId: r.URL.Query().Get("campaignId"),
		Limit:      int32(parseLimit(r.URL.Query().Get("limit"))),
		Cursor:     r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetQuests()))
	for _, q := range resp.GetQuests() {
		items = append(items, questView(q))
	}
	envelope.WriteSuccess(w, tid, paged(items, resp.GetNextCursor()))
}

func (h *Handler) updateQuest(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body questBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Quest.UpdateQuest(ctx, &gamev1.UpdateQuestRequest{
		TenantId: tenant, MerchantId: merchant,
		Quest: body.toProto(chi.URLParam(r, "questId")),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, questView(resp.GetQuest()))
}

func (h *Handler) deleteQuest(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	if _, err := h.core.Quest.DeleteQuest(ctx, &gamev1.DeleteQuestRequest{
		TenantId:   tenant, MerchantId: merchant,
		QuestId: chi.URLParam(r, "questId"),
	}); err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{"deleted": true})
}
