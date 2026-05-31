// Package player implements the consumer BFF's player auth + profile + turns +
// wallet-of-turns surface over Core's PlayerService. Auth (start/verify) is
// public (the player has no token yet); the rest require a verified player JWT.
package player

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

// New builds the player handler.
func New(core *coreclient.Client) *Handler { return &Handler{core: core} }

// Routes mounts the player endpoints. Auth start/verify are public; me/* are
// guarded by RequirePlayer (a verified player JWT).
func (h *Handler) Routes(r chi.Router) {
	r.Post("/players/auth/start", h.startAuth)
	r.Post("/players/auth/verify", h.verifyAuth)

	r.Group(func(r chi.Router) {
		r.Use(auth.RequirePlayer)
		r.Get("/players/me", h.getMe)
		r.Put("/players/me", h.updateMe)
		r.Post("/players/me/contacts", h.addContact)
		r.Get("/players/me/turns", h.getTurns)
	})
}

type identifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (h *Handler) startAuth(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		Identifier identifier `json:"identifier"`
		Method     string     `json:"method"`
		CampaignID string     `json:"campaign_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Player.StartAuth(ctx, &gamev1.StartAuthRequest{
		Scope:        coreclient.Scope(tenant, merchant),
		ContactType:  body.Identifier.Type,
		ContactValue: body.Identifier.Value,
		Method:       body.Method,
		CampaignId:   body.CampaignID,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	data := map[string]any{
		"challenge_id": resp.GetChallengeId(),
		"expires_at":   tsString(resp.GetExpiresAt()),
	}
	if resp.GetDevCode() != "" {
		data["dev_code"] = resp.GetDevCode() // dev mode only; Core omits in prod
	}
	envelope.WriteSuccess(w, tid, data)
}

func (h *Handler) verifyAuth(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
		Proof       string `json:"proof"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Player.VerifyAuth(ctx, &gamev1.VerifyAuthRequest{
		Scope:       coreclient.Scope(tenant, merchant),
		ChallengeId: body.ChallengeID,
		Code:        body.Code,
		Proof:       body.Proof,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"token":  resp.GetToken(),
		"player": playerView(resp.GetPlayer(), resp.GetIdentity()),
	})
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Player.GetProfile(ctx, &gamev1.GetProfileRequest{
		Scope: coreclient.Scope(tenant, merchant), PlayerId: auth.PlayerID(r),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, playerView(resp.GetPlayer(), resp.GetIdentity()))
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		Profile json.RawMessage `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Player.UpdateProfile(ctx, &gamev1.UpdateProfileRequest{
		Scope: coreclient.Scope(tenant, merchant), PlayerId: auth.PlayerID(r),
		Profile: string(orEmptyObj(body.Profile)),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, playerView(resp.GetPlayer(), nil))
}

func (h *Handler) addContact(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		Type   string `json:"type"`
		Value  string `json:"value"`
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Player.AddContact(ctx, &gamev1.AddContactRequest{
		Scope:        coreclient.Scope(tenant, merchant),
		PlayerId:     auth.PlayerID(r),
		IdentityId:   auth.IdentityID(r),
		ContactType:  body.Type,
		ContactValue: body.Value,
		Method:       body.Method,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, identityView(resp.GetIdentity()))
}

func (h *Handler) getTurns(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Player.GetTurnBalance(ctx, &gamev1.GetTurnBalanceRequest{
		Scope: coreclient.Scope(tenant, merchant), PlayerId: auth.PlayerID(r),
		ScopeKey: r.URL.Query().Get("campaign_id"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"scope_key": resp.GetScopeKey(),
		"balance":   resp.GetBalance(),
	})
}
