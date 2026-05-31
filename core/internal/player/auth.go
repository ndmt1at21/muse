// Package player is Core's hosting-layer auth: it turns a phone/email login into
// a verified identity + tenant-scoped player + signed JWT. The contact-
// verification step is pluggable per method (code / otp / magic_link / social),
// the identity resolution reuses the pure gamekit/identity resolver, and token
// issuance uses pkg/token. Challenges are persisted via a ChallengeStore (the
// sqlstore adapter) so verification is single-use and survives restarts.
package player

import (
	"context"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/identity"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
	"github.com/muse/pkg/token"
)

// Challenge is a pending auth attempt (one StartAuth → one VerifyAuth).
type Challenge struct {
	ID           string
	Scope        types.Scope
	ContactType  string
	ContactValue string // normalized
	Method       string
	Secret       string // server-issued expected code/token; "" for auto-verify methods
	CampaignID   string
	Consumed     bool
	Attempts     int
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// ChallengeStore persists challenges. sqlstore.DB implements it.
type ChallengeStore interface {
	CreateChallenge(ctx context.Context, c *Challenge) error
	GetChallenge(ctx context.Context, tenantID, challengeID string) (*Challenge, error)
	// ConsumeChallenge marks it used; returns false if it was already consumed.
	ConsumeChallenge(ctx context.Context, tenantID, challengeID string) (ok bool, err error)
}

// Config tunes the authenticator.
type Config struct {
	ChallengeTTL time.Duration // default 10m
	TokenTTL     time.Duration // default 24h
	JWTSecret    string        // HMAC secret for player tokens (required to issue)
	DevMode      bool          // reveal dev codes in StartAuth responses
}

// Authenticator orchestrates StartAuth/VerifyAuth.
type Authenticator struct {
	challenges ChallengeStore
	resolver   *identity.Resolver
	methods    map[string]Method
	ids        ports.IDGen
	rand       ports.RandSource
	clock      ports.Clock
	cfg        Config
}

// New builds an Authenticator with the default method set (code, otp,
// magic_link, social) registered.
func New(challenges ChallengeStore, resolver *identity.Resolver, ids ports.IDGen, rand ports.RandSource, clock ports.Clock, cfg Config) *Authenticator {
	if cfg.ChallengeTTL <= 0 {
		cfg.ChallengeTTL = 10 * time.Minute
	}
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = 24 * time.Hour
	}
	a := &Authenticator{
		challenges: challenges, resolver: resolver, ids: ids, rand: rand, clock: clock, cfg: cfg,
		methods: map[string]Method{},
	}
	a.Register("code", DevCodeMethod{})
	a.Register("otp", DevCodeMethod{})
	a.Register("magic_link", DevLinkMethod{})
	a.Register("social", SocialStubMethod{})
	return a
}

// Register adds or replaces a verification method (e.g. swap in a real OTP/SMS
// provider for "otp" in production).
func (a *Authenticator) Register(method string, m Method) { a.methods[method] = m }

// StartResult is returned by StartAuth.
type StartResult struct {
	ChallengeID string
	ExpiresAt   time.Time
	DevCode     string // populated only in dev mode by dev-stub methods
}

// StartAuth normalizes the contact, issues a challenge via the chosen method,
// persists it, and returns the challenge id (+ a dev code in dev mode).
func (a *Authenticator) StartAuth(ctx context.Context, scope types.Scope, contactType, contactValue, method, campaignID string) (*StartResult, error) {
	if scope.TenantID == "" {
		return nil, gkerr.New(gkerr.ReasonValidationFailed, "tenant_id is required")
	}
	ct := types.ContactType(contactType)
	norm, ok := types.NormalizeContact(ct, contactValue)
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonValidationFailed, "invalid %s", contactType).
			WithMeta("contact_type", contactType)
	}
	if method == "" {
		method = "code"
	}
	m, ok := a.methods[method]
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonValidationFailed, "unknown auth method %q", method).
			WithMeta("method", method)
	}

	secret, devCode := m.Issue(a.rand, a.ids)
	now := a.clock.Now()
	ch := &Challenge{
		ID: a.ids.NewID("chl"), Scope: scope, ContactType: contactType, ContactValue: norm,
		Method: method, Secret: secret, CampaignID: campaignID,
		ExpiresAt: now.Add(a.cfg.ChallengeTTL), CreatedAt: now,
	}
	if err := a.challenges.CreateChallenge(ctx, ch); err != nil {
		return nil, err
	}
	res := &StartResult{ChallengeID: ch.ID, ExpiresAt: ch.ExpiresAt}
	if a.cfg.DevMode {
		res.DevCode = devCode
	}
	return res, nil
}

// VerifyResult is returned by VerifyAuth.
type VerifyResult struct {
	Token         string
	Player        *types.Player
	Identity      *types.Identity
	IdentityIsNew bool
	PlayerIsNew   bool
}

// VerifyAuth checks a challenge's code/proof, then resolves the identity, upserts
// the tenant player, and issues a player JWT. Single-use: the challenge is
// consumed on the first successful verify.
func (a *Authenticator) VerifyAuth(ctx context.Context, scope types.Scope, challengeID, code, proof string) (*VerifyResult, error) {
	ch, err := a.challenges.GetChallenge(ctx, scope.TenantID, challengeID)
	if err != nil {
		return nil, err
	}
	if ch.Consumed {
		return nil, gkerr.New(gkerr.ReasonUnauthenticated, "challenge already used").WithMeta("challenge_id", challengeID)
	}
	if a.clock.Now().After(ch.ExpiresAt) {
		return nil, gkerr.New(gkerr.ReasonUnauthenticated, "challenge expired").WithMeta("challenge_id", challengeID)
	}
	m, ok := a.methods[ch.Method]
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonValidationFailed, "unknown auth method %q", ch.Method)
	}
	submitted := code
	if submitted == "" {
		submitted = proof
	}
	if !m.Check(ch.Secret, submitted) {
		return nil, gkerr.New(gkerr.ReasonUnauthenticated, "invalid code").WithMeta("challenge_id", challengeID)
	}
	// Single-use: consume before issuing anything.
	consumed, err := a.challenges.ConsumeChallenge(ctx, scope.TenantID, challengeID)
	if err != nil {
		return nil, err
	}
	if !consumed {
		return nil, gkerr.New(gkerr.ReasonUnauthenticated, "challenge already used").WithMeta("challenge_id", challengeID)
	}

	login, err := a.resolver.ResolveLogin(ctx, identity.VerifiedLogin{
		Tenant:  ch.Scope,
		Contact: types.Contact{Type: types.ContactType(ch.ContactType), Value: ch.ContactValue, Verified: true},
	})
	if err != nil {
		return nil, err
	}

	tok, err := token.Sign(a.cfg.JWTSecret, token.Claims{
		TenantID:   login.Player.TenantID,
		MerchantID: login.Player.MerchantID,
		PlayerID:   login.Player.ID,
		IdentityID: login.Identity.ID,
	}, a.clock.Now(), a.cfg.TokenTTL)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "issue token").Wrap(err)
	}

	return &VerifyResult{
		Token: tok, Player: login.Player, Identity: login.Identity,
		IdentityIsNew: login.IdentityIsNew, PlayerIsNew: login.PlayerIsNew,
	}, nil
}

// Resolver exposes the underlying identity resolver for the AddContact path.
func (a *Authenticator) Resolver() *identity.Resolver { return a.resolver }
