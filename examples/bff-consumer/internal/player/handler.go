// Package player implements the consumer BFF's player auth + profile + turns +
// wallet-of-turns surface over Core's PlayerService. Auth (start/verify) is
// public (the player has no token yet); the rest require a verified player JWT.
package player

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	gamev1 "github.com/muse/pkg/gen/game/v1"
	"github.com/muse/pkg/token"
)

// Handler holds the Core client + this BFF's auth state (challenge store, JWT
// signing). Authentication is the BFF's responsibility; Core only resolves the
// player from a verified contact.
type Handler struct {
	core       *coreclient.Client
	jwtSecret  string
	devMode    bool // reveal dev codes in startAuth (local/e2e only)
	tokenTTL   time.Duration
	challenges *challengeStore
}

// New builds the player handler. jwtSecret signs player JWTs; devMode reveals
// dev OTP codes for local/e2e flows.
func New(core *coreclient.Client, jwtSecret string, devMode bool) *Handler {
	return &Handler{
		core: core, jwtSecret: jwtSecret, devMode: devMode,
		tokenTTL: 24 * time.Hour, challenges: newChallengeStore(),
	}
}

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
	if tenant == "" {
		envelope.WriteError(w, tid, invalidArg("tenant scope is required"))
		return
	}
	if contactTypeEnum(body.Identifier.Type) == gamev1.ContactType_CONTACT_TYPE_UNSPECIFIED {
		envelope.WriteError(w, tid, invalidArg("identifier.type must be phone or email"))
		return
	}
	method := body.Method
	if method == "" {
		method = "code"
	}
	switch method {
	case "code", "otp", "magic_link", "social":
	default:
		envelope.WriteError(w, tid, invalidArg("unknown auth method "+method))
		return
	}
	// Issue + persist the challenge in the BFF; verification + token minting also
	// happen here. Core is not involved until ResolvePlayer (on verify).
	secret, devCode := issueSecret(method)
	id := "chl_" + randomHex(12)
	exp := time.Now().Add(challengeTTL)
	h.challenges.put(id, challenge{
		tenantID: tenant, merchantID: merchant,
		contactType: body.Identifier.Type, contactValue: body.Identifier.Value,
		method: method, secret: secret, expiresAt: exp,
	})
	data := map[string]any{"challenge_id": id, "expires_at": exp.Unix()}
	if h.devMode && devCode != "" {
		data["dev_code"] = devCode // local/e2e only
	}
	envelope.WriteSuccess(w, tid, data)
}

func (h *Handler) verifyAuth(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	var body struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
		Proof       string `json:"proof"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ch, ok := h.challenges.take(body.ChallengeID) // single-use
	if !ok {
		envelope.WriteError(w, tid, unauthenticated("invalid or already-used challenge"))
		return
	}
	if time.Now().After(ch.expiresAt) {
		envelope.WriteError(w, tid, unauthenticated("challenge expired"))
		return
	}
	submitted := body.Code
	if submitted == "" {
		submitted = body.Proof
	}
	if !checkSecret(ch.method, ch.secret, submitted) {
		envelope.WriteError(w, tid, unauthenticated("invalid code"))
		return
	}
	// Contact is now verified — ask Core to resolve-or-create the player.
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Player.ResolvePlayer(ctx, &gamev1.ResolvePlayerRequest{
		TenantId:        ch.tenantID, MerchantId: ch.merchantID,
		ContactType:  contactTypeEnum(ch.contactType),
		ContactValue: ch.contactValue,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	p := resp.GetPlayer()
	tok, err := token.Sign(h.jwtSecret, token.Claims{
		TenantID:   p.GetTenantId(),
		MerchantID: p.GetMerchantId(),
		PlayerID:   p.GetId(),
		IdentityID: resp.GetIdentity().GetId(),
	}, time.Now(), h.tokenTTL)
	if err != nil {
		envelope.WriteError(w, tid, internalErr("issue token"))
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"token":  tok,
		"player": playerView(p, resp.GetIdentity()),
	})
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Player.GetProfile(ctx, &gamev1.GetProfileRequest{
		TenantId: tenant, MerchantId: merchant, PlayerId: auth.PlayerID(r),
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
		TenantId: tenant, MerchantId: merchant, PlayerId: auth.PlayerID(r),
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
		TenantId:        tenant, MerchantId: merchant,
		PlayerId:     auth.PlayerID(r),
		IdentityId:   auth.IdentityID(r),
		ContactType:  contactTypeEnum(body.Type),
		ContactValue: body.Value,
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
		TenantId: tenant, MerchantId: merchant, PlayerId: auth.PlayerID(r),
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
