// Package wallet implements the consumer BFF's wallet surface over Core's
// WalletService: the player's per-currency balances, the ledger feed, a game's
// milestone progress, and milestone redeem (manual claim / spend_exchange).
// All endpoints require a verified player JWT — the wallet is per-player.
package wallet

import (
	"encoding/json"
	"net/http"
	"strconv"

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

// New builds the consumer wallet handler.
func New(core *coreclient.Client) *Handler { return &Handler{core: core} }

// Routes mounts the wallet endpoints (all require a verified player JWT).
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequirePlayer)
		r.Get("/wallet", h.balances)
		r.Get("/wallet/ledger", h.ledger)
		r.Get("/games/{gameId}/milestones", h.milestones)
		r.Post("/games/{gameId}/redeem", h.redeem)
	})
}

func (h *Handler) balances(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Wallet.GetWallet(ctx, &gamev1.GetWalletRequest{
		Scope:    coreclient.Scope(tenant, merchant),
		ScopeKey: r.URL.Query().Get("scope_key"),
		PlayerId: auth.PlayerID(r),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{"balances": resp.GetBalances()})
}

func (h *Handler) ledger(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Wallet.GetLedger(ctx, &gamev1.GetLedgerRequest{
		Scope:    coreclient.Scope(tenant, merchant),
		ScopeKey: r.URL.Query().Get("scope_key"),
		PlayerId: auth.PlayerID(r),
		Limit:    int32(atoiDefault(r.URL.Query().Get("limit"), 20)),
		Cursor:   r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetEntries()))
	for _, e := range resp.GetEntries() {
		items = append(items, ledgerView(e))
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"items":      items,
		"pagination": map[string]any{"next_cursor": nilIfEmpty(resp.GetNextCursor()), "has_more": resp.GetNextCursor() != ""},
	})
}

func (h *Handler) milestones(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Wallet.GetMilestones(ctx, &gamev1.GetMilestonesRequest{
		Scope:    coreclient.Scope(tenant, merchant),
		GameId:   chi.URLParam(r, "gameId"),
		PlayerId: auth.PlayerID(r),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	rungs := make([]any, 0, len(resp.GetMilestones()))
	for _, m := range resp.GetMilestones() {
		rungs = append(rungs, map[string]any{
			"milestone_id": m.GetMilestoneId(),
			"threshold":    m.GetThreshold(),
			"prize_id":     m.GetPrizeId(),
			"status":       enumx.Name(m.GetStatus()),
			"progress":     m.GetProgress(),
			"remaining":    m.GetRemaining(),
		})
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"currency":   resp.GetCurrency(),
		"mode":       enumx.Name(resp.GetMode()),
		"balance":    resp.GetBalance(),
		"milestones": rungs,
	})
}

func (h *Handler) redeem(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		MilestoneID string `json:"milestone_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Wallet.Redeem(ctx, &gamev1.RedeemRequest{
		Scope:       coreclient.Scope(tenant, merchant),
		GameId:      chi.URLParam(r, "gameId"),
		MilestoneId: body.MilestoneID,
		PlayerId:    auth.PlayerID(r),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	data := map[string]any{
		"redeemed": resp.GetRedeemed(),
		"mode":     enumx.Name(resp.GetMode()),
		"spent":    resp.GetSpent(),
		"balances": resp.GetBalances(),
	}
	if rw := resp.GetReward(); rw != nil {
		data["reward"] = rewardView(rw)
	}
	envelope.WriteSuccess(w, tid, data)
}

func ledgerView(e *gamev1.LedgerEntry) map[string]any {
	return map[string]any{
		"id":         e.GetId(),
		"currency":   e.GetCurrency(),
		"amount":     e.GetAmount(),
		"reason":     enumx.Name(e.GetReason()),
		"ref_id":     e.GetRefId(),
		"created_at": tsString(e.GetCreatedAt()),
	}
}

// tsString returns the unix-seconds timestamp, or nil when unset (0).
func tsString(unix int64) any {
	if unix == 0 {
		return nil
	}
	return unix
}

func rewardView(r *gamev1.Reward) map[string]any {
	return map[string]any{
		"prize_id":  r.GetPrizeId(),
		"name":      r.GetName(),
		"image":     r.GetImage(),
		"type":      r.GetType(),
		"quantity":  r.GetQuantity(),
		"value":     r.GetValue(),
		"reward_id": r.GetRewardId(),
		"code":      r.GetCode(),
	}
}

func atoiDefault(s string, def int) int {
	if s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
