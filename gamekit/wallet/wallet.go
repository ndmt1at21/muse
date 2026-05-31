// Package wallet holds the pure wallet/points rules: which rewards credit the
// wallet (and in what currency/amount), how a game's wallet scope keys the
// balance, and how a milestone's display status is derived. It depends only on
// types so it is embeddable and unit-testable. Storage, the credit txn, redeem,
// and auto-grant live in the hosting layer over the ports.
package wallet

import (
	"github.com/muse/gamekit/types"
)

// IsWalletReward reports whether a reward type is a wallet currency (credited to
// the ledger) rather than a fulfillable prize.
func IsWalletReward(rewardType string) bool {
	return rewardType == types.RewardPoints || rewardType == types.RewardLuckyItem
}

// CreditFor returns the wallet currency + amount a reward credits. points credit
// the "points" currency by Value (the points amount); lucky_item credits its
// named currency by Quantity. ok=false for non-wallet rewards or zero amounts.
func CreditFor(r types.Reward) (currency string, amount int64, ok bool) {
	switch r.Type {
	case types.RewardPoints:
		amt := r.Value
		if amt == 0 {
			amt = r.Quantity
		}
		return "points", amt, amt > 0
	case types.RewardLuckyItem:
		cur := r.Name
		if cur == "" {
			cur = types.RewardLuckyItem
		}
		amt := r.Quantity
		if amt == 0 {
			amt = 1
		}
		return cur, amt, true
	default:
		return "", 0, false
	}
}

// ScopeKey is the wallet balance key for a game: the campaign (default), merchant,
// or tenant per the game's WalletScope, falling back so a balance always has a
// home. Mirrors the turn/leaderboard scope-key convention.
func ScopeKey(game *types.Game, scope types.Scope) string {
	switch game.WalletScope {
	case types.WalletScopeTenant:
		return scope.TenantID
	case types.WalletScopeMerchant:
		if scope.MerchantID != "" {
			return scope.MerchantID
		}
		return scope.TenantID
	default: // campaign (default)
		if game.CampaignID != "" {
			return game.CampaignID
		}
		if scope.MerchantID != "" {
			return scope.MerchantID
		}
		return scope.TenantID
	}
}

// MilestoneStatus derives a milestone's display status from the player's balance
// and whether it was already granted. For cumulative_unlock the rung "unlocks"
// at the threshold; granted is terminal. For spend_exchange a player can always
// redeem while they hold the threshold (status reflects affordability).
func MilestoneStatus(cfg types.MilestoneConfig, m types.Milestone, balance int64, granted bool) string {
	if cfg.Mode == types.MilestoneSpendExchange {
		if balance >= m.Threshold {
			return types.MilestoneUnlocked
		}
		return types.MilestoneLocked
	}
	// cumulative_unlock
	if granted {
		return types.MilestoneGranted
	}
	if balance >= m.Threshold {
		return types.MilestoneUnlocked
	}
	return types.MilestoneLocked
}
