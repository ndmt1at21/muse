// Package grpcsvc adapts the proto gRPC surface to gamekit engine calls and
// the sqlstore admin paths. It is the only place in Core that knows about gRPC;
// gamekit stays transport-free. Domain errors are mapped to canonical gRPC
// status (with ErrorInfo) via pkg/apierr.
package grpcsvc

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/muse/adapters/redisstore"
	"github.com/muse/adapters/sqlstore"
	"github.com/muse/core/internal/integration"
	"github.com/muse/core/internal/leaderboard"
	"github.com/muse/core/internal/wallet"
	"github.com/muse/gamekit"
	"github.com/muse/gamekit/defaults"
	"github.com/muse/gamekit/engine"
	"github.com/muse/gamekit/identity"
	"github.com/muse/gamekit/ports"
	"github.com/muse/pkg/apierr"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Server implements the Engine, GameAdmin, Reward, Fulfillment, Tenant,
// Merchant, Identity, and Player gRPC services over the gamekit engine +
// sqlstore adapter.
type Server struct {
	gamev1.UnimplementedEngineServiceServer
	gamev1.UnimplementedGameConfigServiceServer
	gamev1.UnimplementedRewardServiceServer
	gamev1.UnimplementedFulfillmentServiceServer
	gamev1.UnimplementedTenantServiceServer
	gamev1.UnimplementedMerchantServiceServer
	gamev1.UnimplementedIdentityServiceServer
	gamev1.UnimplementedPlayerServiceServer
	gamev1.UnimplementedCampaignServiceServer
	gamev1.UnimplementedQuestServiceServer
	gamev1.UnimplementedLeaderboardServiceServer
	gamev1.UnimplementedWalletServiceServer
	gamev1.UnimplementedIntegrationServiceServer

	eng      *engine.Engine
	store    *sqlstore.DB
	cache    *redisstore.Cache // optional; nil disables idempotency
	reg      *gamekit.Registry // quest verifiers (+ game extension points)
	resolver *identity.Resolver
	lb       *leaderboard.Service // optional; nil when leaderboards aren't wired
	wallet   *wallet.Service      // optional; nil when the wallet isn't wired
	intg     *integration.Hub     // optional; CRUD + event dispatch (nil when not wired)
	clock    ports.Clock
	log      *slog.Logger
}

// Deps bundles the collaborators the Server needs beyond the engine.
type Deps struct {
	Resolver    *identity.Resolver
	Registry    *gamekit.Registry    // quest verifiers; may be nil when quests aren't wired
	Leaderboard *leaderboard.Service // may be nil when leaderboards aren't wired
	Wallet      *wallet.Service      // may be nil when the wallet isn't wired
	Integration *integration.Hub     // may be nil when the integration hub isn't wired
	Clock       ports.Clock
}

// NewServer builds the gRPC service. cache may be nil; Phase-4 deps (auth/
// resolver) may be zero when those services aren't wired (e.g. older tests).
func NewServer(eng *engine.Engine, store *sqlstore.DB, cache *redisstore.Cache, deps Deps, log *slog.Logger) *Server {
	clock := deps.Clock
	if clock == nil {
		clock = defaults.Clock{}
	}
	return &Server{
		eng: eng, store: store, cache: cache, reg: deps.Registry,
		resolver: deps.Resolver, lb: deps.Leaderboard, wallet: deps.Wallet,
		intg: deps.Integration, clock: clock, log: log,
	}
}

// fail maps a domain/engine error to a gRPC status error.
func (s *Server) fail(err error) error {
	return apierr.FromDomainError(err).Err()
}

// --- EngineService ---

func (s *Server) StartGame(ctx context.Context, req *gamev1.StartGameRequest) (*gamev1.StartGameResponse, error) {
	scope := scopeFromProto(req)
	res, err := s.eng.Start(ctx, scope, req.GetGameId(), req.GetPlayerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.StartGameResponse{
		SessionId: res.SessionID,
		SeedData:  string(res.SeedData),
		ExpiresAt: ts(res.ExpiresAt),
	}, nil
}

func (s *Server) Play(ctx context.Context, req *gamev1.PlayRequest) (*gamev1.PlayResponse, error) {
	scope := scopeFromProto(req)
	traceID := traceIDFrom(ctx)

	// Idempotency: a retried Play with the same key returns the stored result.
	idemKey := ""
	if k := req.GetIdempotencyKey(); k != "" && s.cache != nil {
		idemKey = "idem:" + scope.TenantID + ":" + k
		if b, err := s.cache.Get(ctx, idemKey); err == nil {
			var cached gamev1.PlayResponse
			if json.Unmarshal(b, &cached) == nil {
				return &cached, nil
			}
		}
	}

	res, err := s.eng.Play(ctx, scope, req.GetGameId(), req.GetSessionId(), req.GetPlayerId(), payloadJSON(req.GetPayload()), traceID)
	if err != nil {
		return nil, s.fail(err)
	}
	resp := &gamev1.PlayResponse{
		PlayId:   res.PlayID,
		Rewards:  rewardsToProto(res.Rewards),
		Metadata: metadataToStruct(res.Metadata),
	}
	if idemKey != "" {
		if b, mErr := json.Marshal(resp); mErr == nil {
			_ = s.cache.Set(ctx, idemKey, b, 10*time.Minute)
		}
	}
	return resp, nil
}

func (s *Server) CheckEligibility(ctx context.Context, req *gamev1.CheckEligibilityRequest) (*gamev1.CheckEligibilityResponse, error) {
	scope := scopeFromProto(req)
	el, err := s.eng.Eligibility(ctx, scope, req.GetGameId(), req.GetPlayerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CheckEligibilityResponse{
		CanPlay:        el.CanPlay,
		RemainingPlays: int32(el.RemainingPlays),
		Reason:         el.Reason,
		NextResetAt:    tsPtr(el.NextResetAt),
	}, nil
}

func (s *Server) GetHistory(ctx context.Context, req *gamev1.GetHistoryRequest) (*gamev1.GetHistoryResponse, error) {
	scope := scopeFromProto(req)
	entries, next, err := s.eng.History(ctx, scope, req.GetGameId(), req.GetPlayerId(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.HistoryEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, &gamev1.HistoryEntry{
			PlayId:    e.ID,
			GameId:    e.GameID,
			Rewards:   string(e.Rewards),
			Metadata:  string(e.Metadata),
			CreatedAt: ts(e.CreatedAt),
		})
	}
	return &gamev1.GetHistoryResponse{Entries: out, NextCursor: next}, nil
}

// --- GameConfigService ---

func (s *Server) CreateGame(ctx context.Context, req *gamev1.CreateGameRequest) (*gamev1.CreateGameResponse, error) {
	scope := scopeFromProto(req)
	g := gameFromProto(scope, req.GetGame())
	if g.ID == "" {
		g.ID = "game_" + randSuffix()
	}
	saved, err := s.store.CreateGame(ctx, g)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CreateGameResponse{Game: gameToProto(saved)}, nil
}

func (s *Server) GetGame(ctx context.Context, req *gamev1.GetGameRequest) (*gamev1.GetGameResponse, error) {
	scope := scopeFromProto(req)
	g, err := s.store.GetGame(ctx, scope, req.GetGameId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetGameResponse{Game: gameToProto(g)}, nil
}

// --- RewardService ---

func (s *Server) CreatePrize(ctx context.Context, req *gamev1.CreatePrizeRequest) (*gamev1.CreatePrizeResponse, error) {
	scope := scopeFromProto(req)
	p := prizeFromProto(scope, req.GetPrize())
	if p.ID == "" {
		p.ID = "prize_" + randSuffix()
	}
	saved, err := s.store.CreatePrize(ctx, p, req.GetGameId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CreatePrizeResponse{Prize: prizeToProto(saved)}, nil
}

func (s *Server) ListPrizes(ctx context.Context, req *gamev1.ListPrizesRequest) (*gamev1.ListPrizesResponse, error) {
	scope := scopeFromProto(req)
	prizes, err := s.store.ListPrizes(ctx, scope, req.GetGameId())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.Prize, 0, len(prizes))
	for i := range prizes {
		out = append(out, prizeToProto(&prizes[i]))
	}
	return &gamev1.ListPrizesResponse{Prizes: out}, nil
}
