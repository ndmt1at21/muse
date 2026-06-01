package grpcsvc

import (
	"context"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// --- WalletService: player wallet/ledger/milestones + redeem (Phase 8) ---

func (s *Server) walletReady() error {
	if s.wallet == nil {
		return s.fail(gkerr.New(gkerr.ReasonInternal, "wallet service not configured"))
	}
	return nil
}

func (s *Server) GetWallet(ctx context.Context, req *gamev1.GetWalletRequest) (*gamev1.GetWalletResponse, error) {
	if err := s.walletReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req)
	balances, err := s.wallet.Balances(ctx, scope.TenantID, req.GetScopeKey(), req.GetPlayerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetWalletResponse{Balances: balances}, nil
}

func (s *Server) GetLedger(ctx context.Context, req *gamev1.GetLedgerRequest) (*gamev1.GetLedgerResponse, error) {
	if err := s.walletReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req)
	entries, next, err := s.wallet.Ledger(ctx, scope.TenantID, req.GetScopeKey(), req.GetPlayerId(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.LedgerEntry, 0, len(entries))
	for i := range entries {
		out = append(out, ledgerEntryToProto(&entries[i]))
	}
	return &gamev1.GetLedgerResponse{Entries: out, NextCursor: next}, nil
}

func (s *Server) GetMilestones(ctx context.Context, req *gamev1.GetMilestonesRequest) (*gamev1.GetMilestonesResponse, error) {
	if err := s.walletReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req)
	res, err := s.wallet.Milestones(ctx, scope, req.GetGameId(), req.GetPlayerId())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.MilestoneState, 0, len(res.Milestones))
	for i := range res.Milestones {
		mv := &res.Milestones[i]
		out = append(out, &gamev1.MilestoneState{
			MilestoneId: mv.Milestone.ID,
			Threshold:   mv.Milestone.Threshold,
			PrizeId:     mv.Milestone.PrizeID,
			Status:      pbMilestoneStatus(mv.Status),
			Progress:    mv.Progress,
			Remaining:   mv.Remaining,
		})
	}
	return &gamev1.GetMilestonesResponse{
		Currency:   res.Currency,
		Mode:       pbMilestoneMode(res.Mode),
		Balance:    res.Balance,
		Milestones: out,
	}, nil
}

func (s *Server) Redeem(ctx context.Context, req *gamev1.RedeemRequest) (*gamev1.RedeemResponse, error) {
	if err := s.walletReady(); err != nil {
		return nil, err
	}
	scope := scopeFromProto(req)
	res, err := s.wallet.Redeem(ctx, scope, req.GetGameId(), req.GetMilestoneId(), req.GetPlayerId())
	if err != nil {
		return nil, s.fail(err)
	}
	resp := &gamev1.RedeemResponse{
		Redeemed: res.Redeemed,
		Mode:     pbMilestoneMode(res.Mode),
		Spent:    res.Spent,
		Balances: res.Balances,
	}
	if rp := rewardsToProto([]types.Reward{res.Reward}); len(rp) > 0 {
		resp.Reward = rp[0]
	}
	return resp, nil
}

func ledgerEntryToProto(e *types.LedgerEntry) *gamev1.LedgerEntry {
	return &gamev1.LedgerEntry{
		Id:        e.ID,
		Currency:  e.Currency,
		Amount:    e.Amount,
		Reason:    pbLedgerReason(e.Reason),
		RefId:     e.RefID,
		CreatedAt: ts(e.CreatedAt),
	}
}
