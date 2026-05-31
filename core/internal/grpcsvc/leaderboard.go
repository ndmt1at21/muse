package grpcsvc

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// --- LeaderboardService: admin CRUD/finalize/reset/disqualify/adjust + player reads ---

func (s *Server) lbReady() error {
	if s.lb == nil {
		return s.fail(gkerr.New(gkerr.ReasonInternal, "leaderboard service not configured"))
	}
	return nil
}

func (s *Server) CreateLeaderboard(ctx context.Context, req *gamev1.CreateLeaderboardRequest) (*gamev1.CreateLeaderboardResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	if scope.TenantID == "" || scope.MerchantID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id and merchant_id are required"))
	}
	lb, err := s.lb.Create(ctx, leaderboardFromProto(scope, req.GetLeaderboard()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CreateLeaderboardResponse{Leaderboard: leaderboardToProto(lb)}, nil
}

func (s *Server) GetLeaderboard(ctx context.Context, req *gamev1.GetLeaderboardRequest) (*gamev1.GetLeaderboardResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	lb, err := s.lb.Get(ctx, scope.TenantID, scope.MerchantID, req.GetLeaderboardId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetLeaderboardResponse{Leaderboard: leaderboardToProto(lb)}, nil
}

func (s *Server) UpdateLeaderboard(ctx context.Context, req *gamev1.UpdateLeaderboardRequest) (*gamev1.UpdateLeaderboardResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	if scope.TenantID == "" || scope.MerchantID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id and merchant_id are required"))
	}
	lb, err := s.lb.Update(ctx, leaderboardFromProto(scope, req.GetLeaderboard()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.UpdateLeaderboardResponse{Leaderboard: leaderboardToProto(lb)}, nil
}

func (s *Server) ListLeaderboards(ctx context.Context, req *gamev1.ListLeaderboardsRequest) (*gamev1.ListLeaderboardsResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	lbs, next, err := s.lb.List(ctx, scope.TenantID, scope.MerchantID, req.GetCampaignId(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.Leaderboard, 0, len(lbs))
	for i := range lbs {
		out = append(out, leaderboardToProto(&lbs[i]))
	}
	return &gamev1.ListLeaderboardsResponse{Leaderboards: out, NextCursor: next}, nil
}

func (s *Server) FinalizeLeaderboard(ctx context.Context, req *gamev1.FinalizeLeaderboardRequest) (*gamev1.FinalizeLeaderboardResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	awards, err := s.lb.Finalize(ctx, scope, req.GetLeaderboardId())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.TierAward, 0, len(awards))
	for _, a := range awards {
		out = append(out, &gamev1.TierAward{Rank: int32(a.Rank), PlayerId: a.PlayerID, PrizeId: a.PrizeID, RewardId: a.RewardID})
	}
	// Best-effort domain event → integration hub (Phase 10).
	s.emit(ctx, types.EventLeaderboardFinalized, scope, map[string]any{
		"leaderboard_id": req.GetLeaderboardId(), "awarded": len(awards),
	})
	return &gamev1.FinalizeLeaderboardResponse{Awarded: int32(len(awards)), Awards: out}, nil
}

func (s *Server) ResetLeaderboard(ctx context.Context, req *gamev1.ResetLeaderboardRequest) (*gamev1.ResetLeaderboardResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	n, err := s.lb.Reset(ctx, scope, req.GetLeaderboardId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.ResetLeaderboardResponse{Cleared: n}, nil
}

func (s *Server) DisqualifyEntry(ctx context.Context, req *gamev1.DisqualifyEntryRequest) (*gamev1.DisqualifyEntryResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	e, err := s.lb.Disqualify(ctx, scope, req.GetLeaderboardId(), req.GetPlayerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.DisqualifyEntryResponse{Entry: entryToProto(e, 0)}, nil
}

func (s *Server) AdjustEntry(ctx context.Context, req *gamev1.AdjustEntryRequest) (*gamev1.AdjustEntryResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	e, err := s.lb.Adjust(ctx, scope, req.GetLeaderboardId(), req.GetPlayerId(), req.GetDelta())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.AdjustEntryResponse{Entry: entryToProto(e, 0)}, nil
}

func (s *Server) GetRankings(ctx context.Context, req *gamev1.GetRankingsRequest) (*gamev1.GetRankingsResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	entries, total, wk, err := s.lb.Rankings(ctx, scope, req.GetLeaderboardId(), int(req.GetLimit()), int(req.GetOffset()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetRankingsResponse{Entries: rankedToProto(entries), Total: total, WindowKey: wk}, nil
}

func (s *Server) AroundMe(ctx context.Context, req *gamev1.AroundMeRequest) (*gamev1.AroundMeResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	entries, err := s.lb.AroundMe(ctx, scope, req.GetLeaderboardId(), req.GetPlayerId(), int(req.GetRadius()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.AroundMeResponse{Entries: rankedToProto(entries)}, nil
}

func (s *Server) MyRank(ctx context.Context, req *gamev1.MyRankRequest) (*gamev1.MyRankResponse, error) {
	if err := s.lbReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req.GetScope())
	entry, nextFrom, toGo, err := s.lb.MyRank(ctx, scope, req.GetLeaderboardId(), req.GetPlayerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.MyRankResponse{
		Entry:            entryToProto(&entry.LeaderboardEntry, entry.Rank),
		NextTierFromRank: int32(nextFrom),
		RanksToNextTier:  toGo,
	}, nil
}

// --- converters ---

func leaderboardFromProto(scope types.Scope, p *gamev1.Leaderboard) *types.Leaderboard {
	if p == nil {
		return &types.Leaderboard{TenantID: scope.TenantID, MerchantID: scope.MerchantID}
	}
	lb := &types.Leaderboard{
		ID:         p.GetId(),
		TenantID:   scope.TenantID,
		MerchantID: scope.MerchantID,
		CampaignID: p.GetCampaignId(),
		Name:       p.GetName(),
		Metric:     p.GetMetric(),
		Status:     p.GetStatus(),
	}
	if s := p.GetTimeWindow(); s != "" {
		_ = json.Unmarshal([]byte(s), &lb.Window)
	}
	if s := p.GetPrizeTiers(); s != "" {
		_ = json.Unmarshal([]byte(s), &lb.PrizeTiers)
	}
	if s := p.GetAntiCheat(); s != "" {
		_ = json.Unmarshal([]byte(s), &lb.AntiCheat)
	}
	return lb
}

func leaderboardToProto(lb *types.Leaderboard) *gamev1.Leaderboard {
	window, _ := json.Marshal(lb.Window)
	tiers, _ := json.Marshal(lb.PrizeTiers)
	ac, _ := json.Marshal(lb.AntiCheat)
	return &gamev1.Leaderboard{
		Id:         lb.ID,
		TenantId:   lb.TenantID,
		MerchantId: lb.MerchantID,
		CampaignId: lb.CampaignID,
		Name:       lb.Name,
		Metric:     lb.Metric,
		TimeWindow: string(window),
		PrizeTiers: string(tiers),
		AntiCheat:  string(ac),
		Status:     lb.Status,
		CreatedAt:  ts(lb.CreatedAt),
		UpdatedAt:  ts(lb.UpdatedAt),
	}
}

func entryToProto(e *types.LeaderboardEntry, rank int64) *gamev1.RankedEntry {
	return &gamev1.RankedEntry{
		PlayerId: e.PlayerID, Score: e.Score, Plays: e.Plays, Rank: rank, State: e.State,
	}
}

func rankedToProto(entries []types.RankedEntry) []*gamev1.RankedEntry {
	out := make([]*gamev1.RankedEntry, 0, len(entries))
	for i := range entries {
		out = append(out, entryToProto(&entries[i].LeaderboardEntry, entries[i].Rank))
	}
	return out
}
