// Package types holds the pure domain models for the game engine SDK.
// No transport, persistence, or framework concerns leak in here.
package types

import (
	"encoding/json"
	"time"
)

// Scope is the tenant/merchant isolation boundary threaded through every
// engine call and every port method. Adapters MUST filter every query by it.
type Scope struct {
	TenantID   string `json:"tenant_id"`
	MerchantID string `json:"merchant_id"`
}

// GameStatus is the lifecycle state of a game.
type GameStatus string

const (
	StatusDraft  GameStatus = "draft"
	StatusActive GameStatus = "active"
	StatusPaused GameStatus = "paused"
	StatusEnded  GameStatus = "ended"
)

// Rules are the engine-enforced gameplay constraints (eligibility window + caps).
type Rules struct {
	MaxPlaysPerUser int        `json:"max_plays_per_user"`
	MaxPlaysPerDay  int        `json:"max_plays_per_day"`
	RequireLogin    bool       `json:"require_login"`
	StartDate       *time.Time `json:"start_date,omitempty"`
	EndDate         *time.Time `json:"end_date,omitempty"`
	// UseTurns gates the game on the player's quest-granted turn balance (keyed
	// by campaign): eligibility reads the balance and Play consumes one turn.
	// Requires the host to wire a turn store into the engine; ignored in a pure
	// embed built without one.
	UseTurns bool `json:"use_turns,omitempty"`
}

// Game is the config-driven definition of a playable game. The engine never
// changes per game shape: seed_generator / reward_handler / validator name
// registry entries, and handler_config is opaque JSON decoded by the handler.
type Game struct {
	ID              string          `json:"id"`
	Scope           Scope           `json:"scope"`
	CampaignID      string          `json:"campaign_id"`
	Name            string          `json:"name"`
	Type            string          `json:"type"` // spin_wheel, scratch_card, egg_catcher, ...
	SeedGenerator   string          `json:"seed_generator"`
	RewardHandler   string          `json:"reward_handler"`
	Validator       string          `json:"validator"`
	Status          GameStatus      `json:"status"`
	Rules           Rules           `json:"rules"`
	HandlerConfig   json.RawMessage `json:"handler_config"`   // shape-specific, decoded by the handler
	ValidatorConfig json.RawMessage `json:"validator_config"` // anti-cheat params, decoded by the validator
	// UI is opaque render config (background, theme, slot/item images). The engine
	// never interprets it; it is stored verbatim and surfaced to the BFF/widget.
	UI json.RawMessage `json:"ui,omitempty"`
	// WalletScope keys this game's wallet credits (campaign|merchant|tenant;
	// "" → campaign). Milestones is the per-game milestone config (currency,
	// mode, auto_grant, thresholds→prizes), decoded by the wallet layer.
	WalletScope string          `json:"wallet_scope,omitempty"`
	Milestones  json.RawMessage `json:"milestones,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// Prize is an awardable item with finite stock. Stock integrity is enforced by
// the PrizeStore.Deduct atomic conditional update, never in application memory.
type Prize struct {
	ID          string           `json:"id"`
	Scope       Scope            `json:"scope"`
	Name        string           `json:"name"`
	Type        string           `json:"type"` // voucher, physical, points, lucky_item, none
	Image       string           `json:"image"`
	Value       int64            `json:"value"`
	Total       int64            `json:"total"`
	Remaining   int64            `json:"remaining"`
	Constraints PrizeConstraints `json:"constraints"`
	Fulfillment PrizeFulfillment `json:"fulfillment"`
	Metadata    json.RawMessage  `json:"metadata,omitempty"`
}

// PrizeConstraints cap how often a single player may win a prize. A zero value
// means "unlimited" for that dimension. Enforced inside the Play transaction.
type PrizeConstraints struct {
	MaxPerUser int `json:"max_per_user"`
	MaxPerDay  int `json:"max_per_day"`
}

// PrizeFulfillment declares how a won prize is redeemed and delivered. It is
// stored as an opaque JSON blob on the prize; the engine reads only the bits
// that decide reward status (RedemptionMode) and whether an outbox delivery
// task is needed (Channel). Everything else is for the dispatcher/providers.
type PrizeFulfillment struct {
	// RedemptionMode: instant (auto-deliver on win) | on_claim (player claims)
	// | manual (staff approval) | exchange (milestone redeem). Default on_claim.
	RedemptionMode string `json:"redemption_mode"`
	// Channel is the delivery channel handled by a FulfillmentProvider:
	// voucher_code | sms | zns | email | points_credit | physical_shipping |
	// crm_sync | ecommerce | external_workflow. Empty/none = in-app only.
	Channel string `json:"channel,omitempty"`
	// Method is the legacy Phase-3 field: "code" maps to the voucher_code
	// channel. Kept for backward compatibility; Channel takes precedence.
	Method string `json:"method,omitempty"`
	// ChannelConfig is opaque per-channel config (webhook_url, hmac_secret_ref,
	// collect_fields, ...), passed through to the provider unchanged.
	ChannelConfig json.RawMessage `json:"channel_config,omitempty"`
	// Retry tunes the dispatcher's backoff/dead-letter for this prize's tasks.
	Retry RetryPolicy `json:"retry,omitzero"`
	// Notification lists secondary notify channels (zns/sms/...) — advisory.
	Notification []string `json:"notification,omitempty"`
}

// RetryPolicy bounds the dispatcher's redelivery attempts for a task.
type RetryPolicy struct {
	MaxAttempts int    `json:"max_attempts,omitempty"` // 0 = use dispatcher default
	Backoff     string `json:"backoff,omitempty"`      // "exponential" (default) | "fixed"
}

// Redemption modes.
const (
	RedemptionInstant  = "instant"
	RedemptionOnClaim  = "on_claim"
	RedemptionManual   = "manual"
	RedemptionExchange = "exchange"
)

// Delivery channels handled by FulfillmentProviders (PLAN §"Prize fulfillment").
const (
	ChannelVoucherCode      = "voucher_code"
	ChannelSMS              = "sms"
	ChannelZNS              = "zns"
	ChannelEmail            = "email"
	ChannelPointsCredit     = "points_credit"
	ChannelPhysicalShipping = "physical_shipping"
	ChannelCRMSync          = "crm_sync"
	ChannelEcommerce        = "ecommerce"
	ChannelExternalWorkflow = "external_workflow"
)

// ResolvedChannel returns the effective delivery channel, mapping the legacy
// Method=="code" to voucher_code. Empty means in-app only (no delivery task).
func (f PrizeFulfillment) ResolvedChannel() string {
	if f.Channel != "" {
		return f.Channel
	}
	if f.Method == "code" {
		return ChannelVoucherCode
	}
	return ""
}

// NeedsAsyncDelivery reports whether a won unit requires an out-of-band delivery
// task in the transactional outbox. voucher_code (and none) are delivered in-app
// — the code is assigned synchronously at win and shown in the play result — so
// they need no task; every other channel hands off to the dispatcher/providers.
func (f PrizeFulfillment) NeedsAsyncDelivery() bool {
	switch f.ResolvedChannel() {
	case ChannelSMS, ChannelZNS, ChannelEmail, ChannelPointsCredit,
		ChannelPhysicalShipping, ChannelCRMSync, ChannelEcommerce, ChannelExternalWorkflow:
		return true
	default:
		return false
	}
}

// RewardStatus is the lifecycle state of a persisted reward record.
type RewardStatus string

const (
	RewardWon       RewardStatus = "won"
	RewardClaimed   RewardStatus = "claimed"
	RewardFulfilled RewardStatus = "fulfilled"
	RewardRevoked   RewardStatus = "revoked"
)

// RewardRecord is a durable record that a player won one unit of a prize. It
// carries the redemption lifecycle and any assigned voucher code. One play may
// produce several reward records (e.g. gift-catcher catching multiple items).
type RewardRecord struct {
	ID          string          `json:"id"`
	Scope       Scope           `json:"scope"`
	GameID      string          `json:"game_id"`
	PlayerID    string          `json:"player_id"`
	PrizeID     string          `json:"prize_id"`
	PlayID      string          `json:"play_id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Value       int64           `json:"value"`
	Code        string          `json:"code,omitempty"`
	Status      RewardStatus    `json:"status"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	ClaimedAt   *time.Time      `json:"claimed_at,omitempty"`
	FulfilledAt *time.Time      `json:"fulfilled_at,omitempty"`
	RevokedAt   *time.Time      `json:"revoked_at,omitempty"`
}

// TaskStatus is the lifecycle state of a fulfillment outbox task.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"    // awaiting (or retrying) dispatch
	TaskProcessing TaskStatus = "processing" // handed to a provider; for external_workflow, awaiting callback
	TaskFulfilled  TaskStatus = "fulfilled"  // delivered (terminal, success)
	TaskFailed     TaskStatus = "failed"     // attempt failed, will retry (transient)
	TaskDead       TaskStatus = "dead"       // gave up after max attempts (terminal, dead-letter)
)

// FulfillmentTask is one row of the transactional outbox: a unit of delivery
// work written in the SAME DB txn as the reward + stock deduction, so a win can
// never be lost or double-delivered. A dispatcher worker polls pending tasks and
// invokes the channel's FulfillmentProvider with retry/backoff and a dead-letter
// terminal state. The task id doubles as the provider idempotency key.
type FulfillmentTask struct {
	ID            string          `json:"id"`
	Scope         Scope           `json:"scope"`
	RewardID      string          `json:"reward_id"`
	PrizeID       string          `json:"prize_id"`
	PlayerID      string          `json:"player_id"`
	GameID        string          `json:"game_id"`
	CampaignID    string          `json:"campaign_id"`
	Channel       string          `json:"channel"`
	ChannelConfig json.RawMessage `json:"channel_config,omitempty"`
	Status        TaskStatus      `json:"status"`
	Attempts      int             `json:"attempts"`
	MaxAttempts   int             `json:"max_attempts"`
	LastError     string          `json:"last_error,omitempty"`
	Receipt       json.RawMessage `json:"receipt,omitempty"` // provider result: voucher code, tracking no., ...
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// SeedData is the server-generated render seed produced by a SeedGenerator at
// Start (e.g. a drop sequence). Opaque to the engine; meaningful to the FE +
// the matching validator/handler.
type SeedData = json.RawMessage

// Payload is the shape-specific client input submitted at Play. Opaque to the
// engine; routed to the configured handler/validator.
type Payload = json.RawMessage

// Session is a server-issued, single-use play ticket bound to player+game.
type Session struct {
	ID        string    `json:"id"`
	Scope     Scope     `json:"scope"`
	GameID    string    `json:"game_id"`
	PlayerID  string    `json:"player_id"`
	SeedData  SeedData  `json:"seed_data"`
	Secret    string    `json:"secret"` // per-session HMAC secret (strict anti-cheat)
	StartedAt time.Time `json:"started_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Consumed  bool      `json:"consumed"`
}

// Reward is a single awarded item returned by a handler. The engine routes by
// Type inside the Play txn (points → wallet ledger, voucher → fulfillment task).
type Reward struct {
	PrizeID  string `json:"prize_id"`
	Name     string `json:"name"`
	Image    string `json:"image"`
	Type     string `json:"type"`
	Quantity int64  `json:"quantity"`
	Value    int64  `json:"value"`
	// Populated by the engine after persistence (when a RewardStore is wired):
	RewardID string `json:"reward_id,omitempty"` // the durable reward record id (for claim)
	Code     string `json:"code,omitempty"`      // assigned voucher code, if any
}

// RewardResult is what a RewardHandler returns: the awarded rewards plus
// shape-specific metadata (slot_index, score+tier, total_caught, ...).
type RewardResult struct {
	Rewards  []Reward       `json:"rewards"`
	Metadata map[string]any `json:"metadata"`
}

// PlayResult is the engine's Play output (mirrors RewardResult after persistence).
type PlayResult struct {
	PlayID   string         `json:"play_id"`
	Rewards  []Reward       `json:"rewards"`
	Metadata map[string]any `json:"metadata"`
}

// StartResult is the engine's Start output.
type StartResult struct {
	SessionID string    `json:"session_id"`
	SeedData  SeedData  `json:"seed_data"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Eligibility reports whether a player may currently play.
type Eligibility struct {
	CanPlay        bool       `json:"can_play"`
	RemainingPlays int        `json:"remaining_plays"`
	Reason         string     `json:"reason,omitempty"` // OUT_OF_TURNS | NOT_STARTED | ENDED
	NextResetAt    *time.Time `json:"next_reset_at,omitempty"`
}

// PlayHistory is the immutable audit record of a single play.
type PlayHistory struct {
	ID        string          `json:"id"`
	Scope     Scope           `json:"scope"`
	GameID    string          `json:"game_id"`
	PlayerID  string          `json:"player_id"`
	SessionID string          `json:"session_id"`
	Payload   Payload         `json:"payload"`
	Rewards   json.RawMessage `json:"rewards"`
	Metadata  json.RawMessage `json:"metadata"`
	TraceID   string          `json:"trace_id"`
	CreatedAt time.Time       `json:"created_at"`
}
