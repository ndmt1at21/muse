package types

import "time"

// --- Wallet, Points & Exchange (Phase 8) ---
//
// A wallet holds named balances (points, lucky_star, coin, …) per player, keyed
// by (tenant_id, scope_key, currency) where scope_key is the campaign / merchant
// / tenant id per the game's wallet_scope (default campaign). Plays credit the
// wallet inside the Play txn for `points` / `lucky_item` rewards; an append-only
// ledger is the audit trail. Milestones convert accumulated balance into prizes,
// either by unlocking at a threshold (cumulative_unlock, points NOT spent) or by
// spending the threshold (spend_exchange, a shop).

// Wallet reward types — rewards of these types are credited to the wallet ledger
// rather than creating a fulfillment task.
const (
	RewardPoints    = "points"
	RewardLuckyItem = "lucky_item"
)

// Milestone redemption modes.
const (
	MilestoneCumulativeUnlock = "cumulative_unlock" // reach threshold → grant once, no spend
	MilestoneSpendExchange    = "spend_exchange"    // redeem spends threshold from balance
)

// Ledger reasons.
const (
	LedgerReasonPlay   = "play"
	LedgerReasonRedeem = "redeem"
	LedgerReasonAdjust = "adjust"
)

// LedgerEntry is one immutable wallet movement (signed amount: + earn, - spend).
type LedgerEntry struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	ScopeKey  string    `json:"scope_key"`
	PlayerID  string    `json:"player_id"`
	Currency  string    `json:"currency"`
	Amount    int64     `json:"amount"`
	Reason    string    `json:"reason"`           // play | redeem | adjust
	RefID     string    `json:"ref_id,omitempty"` // play_id | milestone_id
	CreatedAt time.Time `json:"created_at"`
}

// Milestone is one threshold→prize rung of a milestone config.
type Milestone struct {
	ID        string `json:"milestone_id"`
	Threshold int64  `json:"threshold"`
	PrizeID   string `json:"prize_id"`
}

// MilestoneConfig is the per-game wallet-milestone configuration (stored on the
// game). Currency names which wallet balance it tracks.
type MilestoneConfig struct {
	Currency   string      `json:"currency"`
	Mode       string      `json:"mode"`       // cumulative_unlock | spend_exchange
	AutoGrant  bool        `json:"auto_grant"` // grant at threshold crossing inside Play (cumulative_unlock)
	Milestones []Milestone `json:"milestones"`
}

// MilestoneGrant records that a player was granted a (cumulative_unlock)
// milestone once — the idempotency guard for auto-grant + manual claim.
type MilestoneGrant struct {
	TenantID    string    `json:"tenant_id"`
	ScopeKey    string    `json:"scope_key"`
	PlayerID    string    `json:"player_id"`
	MilestoneID string    `json:"milestone_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// Milestone display statuses (for the milestones query).
const (
	MilestoneLocked   = "locked"   // not yet reached
	MilestoneUnlocked = "unlocked" // reached, awaiting claim (manual)
	MilestoneGranted  = "granted"  // reached & awarded
)
