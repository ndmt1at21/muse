package grpcsvc

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// --- PlayerService: auth, profile, contacts, turn balances ---

func (s *Server) StartAuth(ctx context.Context, req *gamev1.StartAuthRequest) (*gamev1.StartAuthResponse, error) {
	if s.auth == nil {
		return nil, s.fail(gkerr.New(gkerr.ReasonInternal, "auth not configured"))
	}
	scope := scopeFromProto(req.GetScope())
	res, err := s.auth.StartAuth(ctx, scope, req.GetContactType(), req.GetContactValue(), req.GetMethod(), req.GetCampaignId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.StartAuthResponse{
		ChallengeId: res.ChallengeID,
		ExpiresAt:   ts(res.ExpiresAt),
		DevCode:     res.DevCode,
	}, nil
}

func (s *Server) VerifyAuth(ctx context.Context, req *gamev1.VerifyAuthRequest) (*gamev1.VerifyAuthResponse, error) {
	if s.auth == nil {
		return nil, s.fail(gkerr.New(gkerr.ReasonInternal, "auth not configured"))
	}
	scope := scopeFromProto(req.GetScope())
	res, err := s.auth.VerifyAuth(ctx, scope, req.GetChallengeId(), req.GetCode(), req.GetProof())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.VerifyAuthResponse{
		Token:         res.Token,
		Player:        playerToProto(res.Player),
		Identity:      identityToProto(res.Identity),
		IdentityIsNew: res.IdentityIsNew,
		PlayerIsNew:   res.PlayerIsNew,
	}, nil
}

func (s *Server) GetProfile(ctx context.Context, req *gamev1.GetProfileRequest) (*gamev1.GetProfileResponse, error) {
	scope := scopeFromProto(req.GetScope())
	p, err := s.store.GetPlayer(ctx, scope.TenantID, req.GetPlayerId())
	if err != nil {
		return nil, s.fail(err)
	}
	resp := &gamev1.GetProfileResponse{Player: playerToProto(p)}
	if idn, iErr := s.store.GetIdentity(ctx, p.IdentityID); iErr == nil {
		resp.Identity = identityToProto(idn)
	}
	return resp, nil
}

func (s *Server) UpdateProfile(ctx context.Context, req *gamev1.UpdateProfileRequest) (*gamev1.UpdateProfileResponse, error) {
	scope := scopeFromProto(req.GetScope())
	var profile map[string]any
	if raw := req.GetProfile(); raw != "" {
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "profile must be a JSON object"))
		}
	}
	p, err := s.store.UpdatePlayerProfile(ctx, scope.TenantID, req.GetPlayerId(), profile)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.UpdateProfileResponse{Player: playerToProto(p)}, nil
}

// AddContact verifies (dev-stub) + links a second contact to the caller's
// identity, enabling cross-contact login. Reuses the identity resolver's
// LinkContact (which enforces CONTACT_CONFLICT).
func (s *Server) AddContact(ctx context.Context, req *gamev1.AddContactRequest) (*gamev1.AddContactResponse, error) {
	if s.resolver == nil {
		return nil, s.fail(gkerr.New(gkerr.ReasonInternal, "identity resolver not configured"))
	}
	scope := scopeFromProto(req.GetScope())
	identityID := req.GetIdentityId()
	if identityID == "" {
		// Resolve from the player if the caller only supplied player_id.
		p, err := s.store.GetPlayer(ctx, scope.TenantID, req.GetPlayerId())
		if err != nil {
			return nil, s.fail(err)
		}
		identityID = p.IdentityID
	}
	idn, err := s.resolver.LinkContact(ctx, identityID, types.Contact{
		Type: types.ContactType(req.GetContactType()), Value: req.GetContactValue(),
	})
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.AddContactResponse{Identity: identityToProto(idn)}, nil
}

func (s *Server) GetTurnBalance(ctx context.Context, req *gamev1.GetTurnBalanceRequest) (*gamev1.GetTurnBalanceResponse, error) {
	scope := scopeFromProto(req.GetScope())
	scopeKey := turnScopeKey(scope, req.GetScopeKey())
	bal, err := s.store.GetTurnBalance(ctx, scope.TenantID, req.GetPlayerId(), scopeKey)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetTurnBalanceResponse{Balance: bal, ScopeKey: scopeKey}, nil
}

func (s *Server) GrantTurns(ctx context.Context, req *gamev1.GrantTurnsRequest) (*gamev1.GrantTurnsResponse, error) {
	scope := scopeFromProto(req.GetScope())
	scopeKey := turnScopeKey(scope, req.GetScopeKey())
	bal, err := s.store.GrantTurns(ctx, scope.TenantID, req.GetPlayerId(), scopeKey, req.GetDelta())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GrantTurnsResponse{Balance: bal}, nil
}

func (s *Server) ConsumeTurn(ctx context.Context, req *gamev1.ConsumeTurnRequest) (*gamev1.ConsumeTurnResponse, error) {
	scope := scopeFromProto(req.GetScope())
	scopeKey := turnScopeKey(scope, req.GetScopeKey())
	ok, bal, err := s.store.ConsumeTurn(ctx, scope.TenantID, req.GetPlayerId(), scopeKey)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.ConsumeTurnResponse{Ok: ok, Balance: bal}, nil
}

// turnScopeKey falls back to the tenant id when no explicit scope key is given,
// so a balance always has a deterministic home.
func turnScopeKey(scope types.Scope, scopeKey string) string {
	if scopeKey != "" {
		return scopeKey
	}
	if scope.MerchantID != "" {
		return scope.MerchantID
	}
	return scope.TenantID
}
