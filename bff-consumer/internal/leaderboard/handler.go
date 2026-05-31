// Package leaderboard implements the consumer BFF's leaderboard read surface
// over Core's LeaderboardService: public top-N rankings, and the player's
// around-me / my-rank views (which require a verified player JWT).
package leaderboard

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/cache"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Handler holds the Core client and the read-model cache (nil = no caching).
type Handler struct {
	core  *coreclient.Client
	cache *cache.Cache
}

// New builds the consumer leaderboard handler. rmcache may be nil; it caches the
// public top-N rankings with a short TTL.
func New(core *coreclient.Client, rmcache *cache.Cache) *Handler {
	return &Handler{core: core, cache: rmcache}
}

// Routes mounts the leaderboard read endpoints. Rankings is public; around-me
// and my-rank require a verified player JWT.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/leaderboards/{lbId}/rankings", h.rankings)

	r.Group(func(r chi.Router) {
		r.Use(auth.RequirePlayer)
		r.Get("/leaderboards/{lbId}/around-me", h.aroundMe)
		r.Get("/leaderboards/{lbId}/my-rank", h.myRank)
	})
}

// rankings serves the public top-N board, cache-aside with a short TTL: the hot
// "Đua Top" view is read far more than it changes, so a few seconds of caching
// sheds repeated Core round-trips without needing explicit invalidation.
func (h *Handler) rankings(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	lbID := chi.URLParam(r, "lbId")
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)

	data, err := h.cache.Fetch(ctx, cache.LeaderboardRankingsKey(lbID, limit, offset), func() (any, error) {
		resp, gErr := h.core.Leaderboard.GetRankings(ctx, &gamev1.GetRankingsRequest{
			Scope:         coreclient.Scope(tenant, merchant),
			LeaderboardId: lbID,
			Limit:         int32(limit),
			Offset:        int32(offset),
		})
		if gErr != nil {
			return nil, gErr
		}
		items := make([]any, 0, len(resp.GetEntries()))
		for _, e := range resp.GetEntries() {
			items = append(items, entryView(e))
		}
		return map[string]any{
			"items":      items,
			"total":      resp.GetTotal(),
			"window_key": resp.GetWindowKey(),
		}, nil
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, data)
}

func (h *Handler) aroundMe(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.AroundMe(ctx, &gamev1.AroundMeRequest{
		Scope:         coreclient.Scope(tenant, merchant),
		LeaderboardId: chi.URLParam(r, "lbId"),
		PlayerId:      auth.PlayerID(r),
		Radius:        int32(atoiDefault(r.URL.Query().Get("radius"), 3)),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetEntries()))
	for _, e := range resp.GetEntries() {
		items = append(items, entryView(e))
	}
	envelope.WriteSuccess(w, tid, map[string]any{"items": items})
}

func (h *Handler) myRank(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.MyRank(ctx, &gamev1.MyRankRequest{
		Scope:         coreclient.Scope(tenant, merchant),
		LeaderboardId: chi.URLParam(r, "lbId"),
		PlayerId:      auth.PlayerID(r),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	data := entryView(resp.GetEntry())
	data["next_tier_from_rank"] = resp.GetNextTierFromRank()
	data["ranks_to_next_tier"] = resp.GetRanksToNextTier()
	envelope.WriteSuccess(w, tid, data)
}

func entryView(e *gamev1.RankedEntry) map[string]any {
	if e == nil {
		return map[string]any{}
	}
	return map[string]any{
		"player_id": e.GetPlayerId(),
		"score":     e.GetScore(),
		"rank":      e.GetRank(),
		"state":     e.GetState(),
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
