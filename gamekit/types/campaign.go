package types

import "time"

// --- Campaign aggregate (Phase 5) ---
//
// A Campaign lives under a merchant (and thus a tenant) and is the container
// that groups games and quests, configures channels + auth methods, and carries
// the run window. It is a hosting-layer concept — the engine's Play path never
// reads it; the BFFs use it to render the widget and the admin dashboard to
// manage a marketing event. Every row carries tenant_id AND merchant_id (it is
// campaign-scoped), enforced on every query.

// CampaignStatus is the lifecycle state of a campaign.
type CampaignStatus string

const (
	CampaignDraft  CampaignStatus = "draft"
	CampaignActive CampaignStatus = "active"
	CampaignPaused CampaignStatus = "paused"
	CampaignEnded  CampaignStatus = "ended"
)

// Campaign is the container aggregate under a merchant. Channels, Games, and
// Quests are stored as JSON arrays; StartDate/EndDate bound the run window.
type Campaign struct {
	ID         string           `json:"id"`
	TenantID   string           `json:"tenant_id"`
	MerchantID string           `json:"merchant_id"`
	Name       string           `json:"name"`
	Status     CampaignStatus   `json:"status"`
	StartDate  *time.Time       `json:"start_date,omitempty"`
	EndDate    *time.Time       `json:"end_date,omitempty"`
	Channels   []string         `json:"channels,omitempty"` // website_embed | qr_code | chatbot | ...
	Games      []string         `json:"games,omitempty"`    // linked game ids
	Quests     []string         `json:"quests,omitempty"`   // linked quest ids
	Settings   CampaignSettings `json:"settings"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// CampaignSettings are the widget/eligibility knobs surfaced to players and
// enforced by the hosting layer. AuthMethods/CollectFields drive the login +
// lead-capture flow; the max-plays caps bound participation per campaign;
// WalletScope, when set, overrides the tenant/merchant wallet keying.
type CampaignSettings struct {
	RequireAuth     bool     `json:"require_auth,omitempty"`
	AuthMethods     []string `json:"auth_methods,omitempty"`   // phone_otp | zalo | magic_link | ...
	CollectFields   []string `json:"collect_fields,omitempty"` // name | phone | email | ...
	MaxPlaysPerUser int      `json:"max_plays_per_user,omitempty"`
	MaxPlaysPerDay  int      `json:"max_plays_per_day,omitempty"`
	WalletScope     string   `json:"wallet_scope,omitempty"` // campaign | merchant | tenant (override)
}

// CampaignAnalytics is the rollup returned by the analytics-summary query: how
// many plays/wins a campaign's games produced, how many distinct players took
// part, and the win-conversion ratio (wins/plays).
type CampaignAnalytics struct {
	CampaignID    string  `json:"campaign_id"`
	TotalPlays    int64   `json:"total_plays"`
	TotalWins     int64   `json:"total_wins"`
	UniquePlayers int64   `json:"unique_players"`
	Conversion    float64 `json:"conversion"` // wins / plays (0 when no plays)
}
