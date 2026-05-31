package types

import (
	"encoding/json"
	"slices"
	"time"
)

// Canonical domain event types (Phase 10). The engine and hosting services emit
// these; integrations subscribe to them by name. Stable strings — never rename.
const (
	EventPlayCompleted        = "play_completed"
	EventPrizeWon             = "prize_won"
	EventPrizeClaimed         = "prize_claimed"
	EventQuestCompleted       = "quest_completed"
	EventLeaderboardFinalized = "leaderboard_finalized"
)

// Integration adapter types (Phase 10). webhook posts JSON (optionally HMAC
// signed); the rest are stubbed (interfaces with dev impls) pending real
// provider wiring. n8n reuses the external-workflow webhook convention.
const (
	IntegrationWebhook = "webhook"
	IntegrationGSheet  = "gsheet"
	IntegrationSMS     = "sms"
	IntegrationZNS     = "zns"
	IntegrationCRM     = "crm"
	IntegrationN8N     = "n8n"
)

// Integration status.
const (
	IntegrationActive = "active"
	IntegrationPaused = "paused"
)

// Integration is an outbound adapter registered for a tenant/merchant (optionally
// narrowed to one campaign) that fires on the subscribed domain events. Config is
// an opaque adapter-specific JSON blob (webhook url + hmac secret, sheet id, …).
type Integration struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenant_id"`
	MerchantID string          `json:"merchant_id"`
	CampaignID string          `json:"campaign_id,omitempty"` // "" = all campaigns in scope
	Type       string          `json:"type"`
	Events     []string        `json:"events"`
	Config     json.RawMessage `json:"config,omitempty"`
	Status     string          `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
}

// Subscribes reports whether the integration is active and listens for eventType.
func (i *Integration) Subscribes(eventType string) bool {
	if i.Status != IntegrationActive {
		return false
	}
	return slices.Contains(i.Events, eventType)
}
