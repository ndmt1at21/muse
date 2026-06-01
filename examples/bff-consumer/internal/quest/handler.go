// Package quest implements the consumer BFF's player quest surface over Core's
// QuestService: list available quests with completion state, and complete a
// quest (verify proof → grant turns). Listing is public (completion state is
// filled in only when a player token is present); completing requires a verified
// player JWT.
package quest

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

// New builds the consumer quest handler.
func New(core *coreclient.Client) *Handler { return &Handler{core: core} }

// Routes mounts the player quest endpoints. List is public; complete requires
// a verified player JWT.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/quests", h.listQuests)

	r.Group(func(r chi.Router) {
		r.Use(auth.RequirePlayer)
		r.Post("/quests/{questId}/complete", h.completeQuest)
	})
}

func (h *Handler) listQuests(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Quest.ListPlayerQuests(ctx, &gamev1.ListPlayerQuestsRequest{
		TenantId:      tenant, MerchantId: merchant,
		CampaignId: r.URL.Query().Get("campaign_id"),
		PlayerId:   auth.PlayerID(r), // "" for anonymous → completion state omitted
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetQuests()))
	for _, st := range resp.GetQuests() {
		items = append(items, questStateView(st))
	}
	envelope.WriteSuccess(w, tid, map[string]any{"items": items})
}

func (h *Handler) completeQuest(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		Proof json.RawMessage `json:"proof"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // proof is optional (e.g. daily_checkin)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Quest.CompleteQuest(ctx, &gamev1.CompleteQuestRequest{
		TenantId:    tenant, MerchantId: merchant,
		QuestId:  chi.URLParam(r, "questId"),
		PlayerId: auth.PlayerID(r),
		Proof:    string(body.Proof),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"completed":     resp.GetCompleted(),
		"turns_granted": resp.GetTurnsGranted(),
		"turn_balance":  resp.GetTurnBalance(),
	})
}
