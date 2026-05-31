// Package game implements the admin BFF's minimal game + prize config endpoints
// needed to seed the vertical slice. Full CRUD lands in later phases.
package game

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

// New builds the admin handler.
func New(core *coreclient.Client) *Handler { return &Handler{core: core} }

// Routes mounts the admin endpoints under /admin.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/admin/games", h.createGame)
	r.Get("/admin/games/{gameId}", h.getGame)

	r.Post("/admin/prizes", h.createPrize)
	r.Get("/admin/prizes", h.listPrizes)
	r.Get("/admin/prizes/summary", h.prizeSummary)
	r.Get("/admin/prizes/{prizeId}", h.getPrize)
	r.Put("/admin/prizes/{prizeId}", h.updatePrize)
	r.Delete("/admin/prizes/{prizeId}", h.deletePrize)
	r.Post("/admin/prizes/{prizeId}/codes", h.importCodes)

	r.Post("/admin/rewards/{rewardId}/fulfill", h.fulfillReward)
	r.Post("/admin/rewards/{rewardId}/revoke", h.revokeReward)
}

type createGameBody struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	CampaignID    string `json:"campaign_id"`
	SeedGenerator string `json:"seed_generator"`
	RewardHandler string `json:"reward_handler"`
	Validator     string `json:"validator"`
	Status        string `json:"status"`
	Rules         struct {
		MaxPlaysPerUser int  `json:"max_plays_per_user"`
		MaxPlaysPerDay  int  `json:"max_plays_per_day"`
		RequireLogin    bool `json:"require_login"`
	} `json:"rules"`
	HandlerConfig   json.RawMessage `json:"handler_config"`
	ValidatorConfig json.RawMessage `json:"validator_config"`
	WalletScope     string          `json:"wallet_scope"` // campaign|merchant|tenant (override)
	Milestones      json.RawMessage `json:"milestones"`   // lucky_item/points milestone config
}

func (h *Handler) createGame(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)

	var body createGameBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.GameConfig.CreateGame(ctx, &gamev1.CreateGameRequest{
		Scope: coreclient.Scope(tenant, merchant),
		Game: &gamev1.Game{
			Name:            body.Name,
			Type:            body.Type,
			CampaignId:      body.CampaignID,
			SeedGenerator:   body.SeedGenerator,
			RewardHandler:   body.RewardHandler,
			Validator:       body.Validator,
			Status:          enumx.Parse[gamev1.GameStatus](body.Status, gamev1.GameStatus_value),
			HandlerConfig:   string(orEmptyObj(body.HandlerConfig)),
			ValidatorConfig: string(orEmptyObj(body.ValidatorConfig)),
			WalletScope:     enumx.Parse[gamev1.WalletScope](body.WalletScope, gamev1.WalletScope_value),
			Milestones:      string(orEmptyObj(body.Milestones)),
			Rules: &gamev1.Rules{
				MaxPlaysPerUser: int32(body.Rules.MaxPlaysPerUser),
				MaxPlaysPerDay:  int32(body.Rules.MaxPlaysPerDay),
				RequireLogin:    body.Rules.RequireLogin,
			},
		},
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, gameView(resp.GetGame()))
}

func (h *Handler) getGame(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.GameConfig.GetGame(ctx, &gamev1.GetGameRequest{
		Scope: coreclient.Scope(tenant, merchant), GameId: chi.URLParam(r, "gameId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, gameView(resp.GetGame()))
}

type createPrizeBody struct {
	GameID string `json:"game_id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Image  string `json:"image"`
	Value  int64  `json:"value"`
	Stock  struct {
		Total int64 `json:"total"`
	} `json:"stock"`
	Constraints json.RawMessage `json:"constraints"` // {max_per_user, max_per_day}
	Fulfillment json.RawMessage `json:"fulfillment"` // {redemption_mode, method}
	Metadata    json.RawMessage `json:"metadata"`
}

func (h *Handler) createPrize(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)

	var body createPrizeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.CreatePrize(ctx, &gamev1.CreatePrizeRequest{
		Scope:  coreclient.Scope(tenant, merchant),
		GameId: body.GameID,
		Prize: &gamev1.Prize{
			Name:             body.Name,
			Type:             body.Type,
			Image:            body.Image,
			Value:            body.Value,
			Total:            body.Stock.Total,
			Remaining:        body.Stock.Total,
			AwardConstraints: string(orEmptyObj(body.Constraints)),
			Fulfillment:      string(orEmptyObj(body.Fulfillment)),
			Metadata:         string(orEmptyObj(body.Metadata)),
		},
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, prizeView(resp.GetPrize()))
}
