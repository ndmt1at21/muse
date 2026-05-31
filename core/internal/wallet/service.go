// Package wallet is the hosting-layer wallet orchestrator: it implements the
// engine's in-Play WalletRouter (credit points/lucky_item + auto-grant
// cumulative milestones), and serves redeem (manual claim / spend_exchange) and
// the milestone-progress query. Pure rules live in gamekit/wallet; storage and
// the credit txn are behind the ports.
package wallet

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
	wkit "github.com/muse/gamekit/wallet"
)

// Store is the persistence the service needs (satisfied by *sqlstore.DB).
type Store interface {
	ports.WalletStore
	ports.GameStore
	ports.PrizeStore
	ports.RewardStore
	ports.FulfillmentStore
	ports.TxRunner
}

// Service orchestrates the wallet. It is the engine's WalletRouter.
type Service struct {
	store Store
	ids   ports.IDGen
	clock ports.Clock
	log   *slog.Logger
}

// New builds the wallet service.
func New(store Store, ids ports.IDGen, clock ports.Clock, log *slog.Logger) *Service {
	return &Service{store: store, ids: ids, clock: clock, log: log}
}

func (s *Service) milestoneConfig(game *types.Game) (types.MilestoneConfig, bool) {
	var cfg types.MilestoneConfig
	if len(game.Milestones) == 0 {
		return cfg, false
	}
	if err := json.Unmarshal(game.Milestones, &cfg); err != nil || cfg.Currency == "" || len(cfg.Milestones) == 0 {
		return cfg, false
	}
	return cfg, true
}

// Credit implements ports.WalletRouter: credit wallet rewards and auto-grant
// cumulative_unlock milestones, inside the Play transaction. Returns any
// milestone-granted prizes to append to the play result.
func (s *Service) Credit(ctx context.Context, scope types.Scope, game *types.Game, playerID, playID string, rewards []types.Reward) ([]types.Reward, error) {
	scopeKey := wkit.ScopeKey(game, scope)
	credited := false
	for _, r := range rewards {
		if cur, amt, ok := wkit.CreditFor(r); ok {
			if _, err := s.store.Credit(ctx, scope.TenantID, scopeKey, playerID, cur, amt, types.LedgerReasonPlay, playID); err != nil {
				return nil, err
			}
			credited = true
		}
	}
	cfg, ok := s.milestoneConfig(game)
	if !ok || !credited || cfg.Mode != types.MilestoneCumulativeUnlock || !cfg.AutoGrant {
		return nil, nil
	}
	bal, err := s.store.Balance(ctx, scope.TenantID, scopeKey, playerID, cfg.Currency)
	if err != nil {
		return nil, err
	}
	var granted []types.Reward
	for _, m := range cfg.Milestones {
		if bal < m.Threshold {
			continue
		}
		won, gErr := s.store.GrantMilestoneOnce(ctx, &types.MilestoneGrant{
			TenantID: scope.TenantID, ScopeKey: scopeKey, PlayerID: playerID, MilestoneID: m.ID,
		})
		if gErr != nil {
			return nil, gErr
		}
		if !won {
			continue // already granted
		}
		reward, mErr := s.mintPrize(ctx, scope, game.CampaignID, playerID, m.PrizeID, playID)
		if mErr != nil {
			return nil, mErr
		}
		granted = append(granted, reward)
	}
	return granted, nil
}

// RedeemResult is the outcome of a milestone redeem.
type RedeemResult struct {
	Redeemed bool
	Mode     string
	Spent    map[string]int64
	Reward   types.Reward
	Balances map[string]int64
}

// Redeem performs a manual milestone claim (cumulative_unlock) or a
// spend_exchange purchase, atomically: verify threshold/balance → (spend) deduct
// → grant-once / mint prize → fulfillment task.
func (s *Service) Redeem(ctx context.Context, scope types.Scope, gameID, milestoneID, playerID string) (*RedeemResult, error) {
	game, err := s.store.GetGame(ctx, scope, gameID)
	if err != nil {
		return nil, err
	}
	cfg, ok := s.milestoneConfig(game)
	if !ok {
		return nil, gkerr.New(gkerr.ReasonValidationFailed, "game has no milestones")
	}
	var milestone *types.Milestone
	for i := range cfg.Milestones {
		if cfg.Milestones[i].ID == milestoneID {
			milestone = &cfg.Milestones[i]
			break
		}
	}
	if milestone == nil {
		return nil, gkerr.New(gkerr.ReasonNotFound, "milestone not found").WithMeta("milestone_id", milestoneID)
	}
	scopeKey := wkit.ScopeKey(game, scope)
	out := &RedeemResult{Redeemed: true, Mode: cfg.Mode, Spent: map[string]int64{}}

	txErr := s.store.WithTx(ctx, func(ctx context.Context) error {
		switch cfg.Mode {
		case types.MilestoneSpendExchange:
			ok, _, sErr := s.store.Spend(ctx, scope.TenantID, scopeKey, playerID, cfg.Currency, milestone.Threshold, types.LedgerReasonRedeem, milestone.ID)
			if sErr != nil {
				return sErr
			}
			if !ok {
				return gkerr.New(gkerr.ReasonValidationFailed, "insufficient balance").WithMeta("currency", cfg.Currency)
			}
			out.Spent[cfg.Currency] = milestone.Threshold
		default: // cumulative_unlock manual claim
			bal, bErr := s.store.Balance(ctx, scope.TenantID, scopeKey, playerID, cfg.Currency)
			if bErr != nil {
				return bErr
			}
			if bal < milestone.Threshold {
				return gkerr.New(gkerr.ReasonValidationFailed, "threshold not reached").WithMeta("currency", cfg.Currency)
			}
			won, gErr := s.store.GrantMilestoneOnce(ctx, &types.MilestoneGrant{
				TenantID: scope.TenantID, ScopeKey: scopeKey, PlayerID: playerID, MilestoneID: milestone.ID,
			})
			if gErr != nil {
				return gErr
			}
			if !won {
				return gkerr.New(gkerr.ReasonAlreadyExists, "milestone already claimed")
			}
		}
		reward, mErr := s.mintPrize(ctx, scope, game.CampaignID, playerID, milestone.PrizeID, "ms:"+milestone.ID)
		if mErr != nil {
			return mErr
		}
		out.Reward = reward
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	out.Balances, _ = s.store.Balances(ctx, scope.TenantID, scopeKey, playerID)
	return out, nil
}

// MilestoneView is one milestone plus the caller's progress/status.
type MilestoneView struct {
	Milestone types.Milestone
	Status    string
	Progress  int64
	Remaining int64
}

// MilestonesResult is the milestones query response.
type MilestonesResult struct {
	Currency   string
	Mode       string
	Balance    int64
	Milestones []MilestoneView
}

// Milestones returns the game's milestone config with the caller's balance,
// per-rung status, and progress.
func (s *Service) Milestones(ctx context.Context, scope types.Scope, gameID, playerID string) (*MilestonesResult, error) {
	game, err := s.store.GetGame(ctx, scope, gameID)
	if err != nil {
		return nil, err
	}
	cfg, ok := s.milestoneConfig(game)
	if !ok {
		return &MilestonesResult{}, nil
	}
	scopeKey := wkit.ScopeKey(game, scope)
	var bal int64
	granted := map[string]bool{}
	if playerID != "" {
		if bal, err = s.store.Balance(ctx, scope.TenantID, scopeKey, playerID, cfg.Currency); err != nil {
			return nil, err
		}
		if granted, err = s.store.GrantedMilestones(ctx, scope.TenantID, scopeKey, playerID); err != nil {
			return nil, err
		}
	}
	res := &MilestonesResult{Currency: cfg.Currency, Mode: cfg.Mode, Balance: bal}
	for _, m := range cfg.Milestones {
		remaining := max(m.Threshold-bal, 0)
		res.Milestones = append(res.Milestones, MilestoneView{
			Milestone: m,
			Status:    wkit.MilestoneStatus(cfg, m, bal, granted[m.ID]),
			Progress:  bal,
			Remaining: remaining,
		})
	}
	return res, nil
}

// Balances returns all currency balances for a player in a scope.
func (s *Service) Balances(ctx context.Context, tenantID, scopeKey, playerID string) (map[string]int64, error) {
	return s.store.Balances(ctx, tenantID, scopeKey, playerID)
}

// Ledger returns wallet movements (paginated).
func (s *Service) Ledger(ctx context.Context, tenantID, scopeKey, playerID string, limit int, cursor string) ([]types.LedgerEntry, string, error) {
	return s.store.Ledger(ctx, tenantID, scopeKey, playerID, limit, cursor)
}

// mintPrize awards a milestone prize: a durable reward record (+ fulfillment
// task when the prize needs async delivery), mirroring the engine's award path.
// Milestone prizes are not stock-limited (continuous issuance from a balance).
func (s *Service) mintPrize(ctx context.Context, scope types.Scope, campaignID, playerID, prizeID, refID string) (types.Reward, error) {
	prize, err := s.store.GetPrize(ctx, scope, prizeID)
	if err != nil {
		return types.Reward{}, err
	}
	ful := prize.Fulfillment
	instant := ful.RedemptionMode == types.RedemptionInstant
	async := ful.NeedsAsyncDelivery()
	now := s.clock.Now()

	status := types.RewardWon
	var fulfilledAt *time.Time
	if instant && !async {
		status, fulfilledAt = types.RewardFulfilled, &now
	}
	code := ""
	if ful.ResolvedChannel() == types.ChannelVoucherCode {
		if c, ok, cErr := s.store.PopCode(ctx, scope, prize.ID); cErr != nil {
			return types.Reward{}, cErr
		} else if ok {
			code = c
		}
	}
	rec := &types.RewardRecord{
		ID:          s.ids.NewID("rwd"),
		Scope:       scope,
		PlayerID:    playerID,
		PrizeID:     prize.ID,
		PlayID:      refID,
		Name:        prize.Name,
		Type:        prize.Type,
		Value:       prize.Value,
		Code:        code,
		Status:      status,
		CreatedAt:   now,
		FulfilledAt: fulfilledAt,
	}
	if err := s.store.InsertReward(ctx, rec); err != nil {
		return types.Reward{}, err
	}
	if async && instant {
		if err := s.store.EnqueueTask(ctx, &types.FulfillmentTask{
			ID: s.ids.NewID("task"), Scope: scope, RewardID: rec.ID, PrizeID: prize.ID, PlayerID: playerID,
			CampaignID: campaignID, Channel: ful.ResolvedChannel(), ChannelConfig: ful.ChannelConfig,
			Status: types.TaskPending, MaxAttempts: ful.Retry.MaxAttempts, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return types.Reward{}, err
		}
	}
	return types.Reward{
		PrizeID: prize.ID, Name: prize.Name, Image: prize.Image, Type: prize.Type,
		Quantity: 1, Value: prize.Value, RewardID: rec.ID, Code: code,
	}, nil
}
