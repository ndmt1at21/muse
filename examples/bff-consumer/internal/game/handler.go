// Package game implements the consumer BFF's generic game-engine endpoints.
// Each handler aggregates/validates, calls one Core RPC, and renders the
// uniform envelope. Scope/player come from the auth seam (headers for now).
package game

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// Handler holds the Core client and the gameplay rate-limit middleware applied
// to the mutating /start + /play routes (nil = no limiting).
type Handler struct {
	core      *coreclient.Client
	playLimit func(http.Handler) http.Handler
}

// New builds the game handler. playLimit guards /start + /play (pass nil to
// disable, e.g. when Redis is absent).
func New(core *coreclient.Client, playLimit func(http.Handler) http.Handler) *Handler {
	return &Handler{core: core, playLimit: playLimit}
}

// Routes mounts the engine endpoints under /games. /start and /play sit behind
// the gameplay rate limit; the read endpoints are unthrottled here.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		if h.playLimit != nil {
			r.Use(h.playLimit)
		}
		r.Post("/games/{gameId}/start", h.start)
		r.Post("/games/{gameId}/play", h.play)
	})
	r.Get("/games/{gameId}/eligibility", h.eligibility)
	r.Get("/games/{gameId}/history/me", h.history)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	gameID := chi.URLParam(r, "gameId")

	var body struct {
		UserID string `json:"user_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	playerID := firstNonEmpty(body.UserID, auth.PlayerID(r))

	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Engine.StartGame(ctx, &gamev1.StartGameRequest{
		Scope: coreclient.Scope(tenant, merchant), GameId: gameID, PlayerId: playerID,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	var seed any
	if resp.GetSeedData() != "" {
		_ = json.Unmarshal([]byte(resp.GetSeedData()), &seed)
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"session_id": resp.GetSessionId(),
		"seed_data":  seed,
		"expires_at": tsString(resp.GetExpiresAt()),
	})
}

func (h *Handler) play(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	gameID := chi.URLParam(r, "gameId")

	var body struct {
		SessionID string          `json:"session_id"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	payloadStruct, err := jsonToStruct(body.Payload)
	if err != nil {
		envelope.WriteError(w, tid, invalidArg("payload must be a JSON object"))
		return
	}

	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Engine.Play(ctx, &gamev1.PlayRequest{
		Scope:          coreclient.Scope(tenant, merchant),
		GameId:         gameID,
		SessionId:      body.SessionID,
		PlayerId:       auth.PlayerID(r),
		Payload:        payloadStruct,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"rewards":  rewardsJSON(resp.GetRewards()),
		"metadata": resp.GetMetadata().AsMap(),
	})
}

func (h *Handler) eligibility(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	gameID := chi.URLParam(r, "gameId")

	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Engine.CheckEligibility(ctx, &gamev1.CheckEligibilityRequest{
		Scope: coreclient.Scope(tenant, merchant), GameId: gameID, PlayerId: auth.PlayerID(r),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	data := map[string]any{
		"can_play":        resp.GetCanPlay(),
		"remaining_plays": resp.GetRemainingPlays(),
		"reason":          emptyToNil(resp.GetReason()),
	}
	if resp.GetNextResetAt() != nil {
		data["next_reset_at"] = tsString(resp.GetNextResetAt())
	}
	envelope.WriteSuccess(w, tid, data)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	gameID := chi.URLParam(r, "gameId")

	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Engine.GetHistory(ctx, &gamev1.GetHistoryRequest{
		Scope: coreclient.Scope(tenant, merchant), GameId: gameID, PlayerId: auth.PlayerID(r),
		Limit: int32(parseLimit(r)), Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetEntries()))
	for _, e := range resp.GetEntries() {
		items = append(items, map[string]any{
			"play_id":    e.GetPlayId(),
			"game_id":    e.GetGameId(),
			"rewards":    rawJSON(e.GetRewards()),
			"metadata":   rawJSON(e.GetMetadata()),
			"created_at": tsString(e.GetCreatedAt()),
		})
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"items": items,
		"pagination": map[string]any{
			"next_cursor": emptyToNil(resp.GetNextCursor()),
			"has_more":    resp.GetNextCursor() != "",
		},
	})
}

// jsonToStruct converts a raw JSON object into a proto Struct (nil/empty → empty).
func jsonToStruct(raw json.RawMessage) (*structpb.Struct, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return &structpb.Struct{}, nil
	}
	s := &structpb.Struct{}
	if err := s.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	return s, nil
}
