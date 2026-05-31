package grpcsvc

import (
	"context"

	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// --- prize CRUD (admin) ---

func (s *Server) GetPrize(ctx context.Context, req *gamev1.GetPrizeRequest) (*gamev1.GetPrizeResponse, error) {
	scope := scopeFromProto(req.GetScope())
	p, err := s.store.GetPrize(ctx, scope, req.GetPrizeId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetPrizeResponse{Prize: prizeToProto(p)}, nil
}

func (s *Server) UpdatePrize(ctx context.Context, req *gamev1.UpdatePrizeRequest) (*gamev1.UpdatePrizeResponse, error) {
	scope := scopeFromProto(req.GetScope())
	p := prizeFromProto(scope, req.GetPrize())
	saved, err := s.store.UpdatePrize(ctx, p)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.UpdatePrizeResponse{Prize: prizeToProto(saved)}, nil
}

func (s *Server) DeletePrize(ctx context.Context, req *gamev1.DeletePrizeRequest) (*gamev1.DeletePrizeResponse, error) {
	scope := scopeFromProto(req.GetScope())
	if err := s.store.DeletePrize(ctx, scope, req.GetPrizeId()); err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.DeletePrizeResponse{}, nil
}

func (s *Server) ImportCodes(ctx context.Context, req *gamev1.ImportCodesRequest) (*gamev1.ImportCodesResponse, error) {
	scope := scopeFromProto(req.GetScope())
	n, err := s.store.ImportCodes(ctx, scope, req.GetPrizeId(), req.GetCodes())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.ImportCodesResponse{Imported: int32(n)}, nil
}

func (s *Server) GetPrizeSummary(ctx context.Context, req *gamev1.GetPrizeSummaryRequest) (*gamev1.GetPrizeSummaryResponse, error) {
	scope := scopeFromProto(req.GetScope())
	rows, err := s.store.PrizeSummary(ctx, scope, req.GetGameId())
	if err != nil {
		return nil, s.fail(err)
	}
	items := make([]*gamev1.PrizeSummaryItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, &gamev1.PrizeSummaryItem{
			PrizeId: r.PrizeID, Name: r.Name, Total: r.Total, Remaining: r.Remaining,
			Awarded: r.Awarded, CodesAvailable: r.CodesAvailable,
		})
	}
	return &gamev1.GetPrizeSummaryResponse{Items: items}, nil
}

// --- reward lifecycle ---

func (s *Server) ClaimReward(ctx context.Context, req *gamev1.ClaimRewardRequest) (*gamev1.ClaimRewardResponse, error) {
	scope := scopeFromProto(req.GetScope())
	// Ownership check: a player may only claim their own reward.
	if pid := req.GetPlayerId(); pid != "" {
		rec, err := s.store.GetReward(ctx, scope, req.GetRewardId())
		if err != nil {
			return nil, s.fail(err)
		}
		if rec.PlayerID != pid {
			return nil, s.fail(notOwned(req.GetRewardId()))
		}
	}
	rec, err := s.store.ClaimReward(ctx, scope, req.GetRewardId())
	if err != nil {
		return nil, s.fail(err)
	}
	// Best-effort domain event → integration hub (Phase 10).
	s.emit(ctx, types.EventPrizeClaimed, scope, map[string]any{
		"reward_id": rec.ID, "prize_id": rec.PrizeID, "player_id": rec.PlayerID,
	})
	return &gamev1.ClaimRewardResponse{Reward: rewardToProto(rec)}, nil
}

func (s *Server) FulfillReward(ctx context.Context, req *gamev1.FulfillRewardRequest) (*gamev1.FulfillRewardResponse, error) {
	scope := scopeFromProto(req.GetScope())
	rec, err := s.store.FulfillReward(ctx, scope, req.GetRewardId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.FulfillRewardResponse{Reward: rewardToProto(rec)}, nil
}

func (s *Server) RevokeReward(ctx context.Context, req *gamev1.RevokeRewardRequest) (*gamev1.RevokeRewardResponse, error) {
	scope := scopeFromProto(req.GetScope())
	rec, err := s.store.RevokeReward(ctx, scope, req.GetRewardId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.RevokeRewardResponse{Reward: rewardToProto(rec)}, nil
}

func (s *Server) ListRewards(ctx context.Context, req *gamev1.ListRewardsRequest) (*gamev1.ListRewardsResponse, error) {
	scope := scopeFromProto(req.GetScope())
	recs, next, err := s.store.ListRewards(ctx, scope, req.GetPlayerId(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.RewardRecord, 0, len(recs))
	for i := range recs {
		out = append(out, rewardToProto(&recs[i]))
	}
	return &gamev1.ListRewardsResponse{Rewards: out, NextCursor: next}, nil
}
