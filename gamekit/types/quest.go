package types

import (
	"encoding/json"
	"time"
)

// --- Quest / Mission (Phase 6) ---
//
// A Quest lives under a campaign (and merchant/tenant). A player completes it by
// submitting a type-specific proof, which a registered QuestVerifier checks;
// on success the hosting layer grants the quest's reward — currently
// `play_turn`s credited to the player's turn balance for the campaign, which a
// turn-gated game then spends at Play. Quest types: daily_checkin, share_social,
// invite_friend, scan_qr, view_page, answer_question, external_event.

// Quest type identifiers (the verifier registry key).
const (
	QuestDailyCheckin   = "daily_checkin"
	QuestShareSocial    = "share_social"
	QuestInviteFriend   = "invite_friend"
	QuestScanQR         = "scan_qr"
	QuestViewPage       = "view_page"
	QuestAnswerQuestion = "answer_question"
	QuestExternalEvent  = "external_event"
)

// Quest reward types (only play_turn is wired in Phase 6).
const (
	QuestRewardPlayTurn = "play_turn"
)

// Quest status values.
const (
	QuestActive   = "active"
	QuestInactive = "inactive"
)

// QuestReward is what completing a quest grants. Phase 6 supports play_turn
// (Quantity turns credited to the campaign turn balance).
type QuestReward struct {
	Type     string `json:"type"`     // play_turn
	Quantity int64  `json:"quantity"` // turns granted
}

// Quest is the admin-configured mission under a campaign. Config is the
// verifier-specific opaque JSON (e.g. {platforms:[...]} for share_social,
// {answers:[...]} for answer_question), decoded only by the matching verifier.
type Quest struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenant_id"`
	MerchantID string          `json:"merchant_id"`
	CampaignID string          `json:"campaign_id"`
	Type       string          `json:"type"`
	Name       string          `json:"name,omitempty"`
	Status     string          `json:"status"`
	Reward     QuestReward     `json:"reward"`
	Config     json.RawMessage `json:"config,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// QuestCompletion is an immutable record that a player completed a quest once.
// daily_checkin allows one per UTC day; other types allow one per player.
type QuestCompletion struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	QuestID   string    `json:"quest_id"`
	PlayerID  string    `json:"player_id"`
	CreatedAt time.Time `json:"created_at"`
}

// QuestState is a quest plus the caller's completion status (for the player list).
type QuestState struct {
	Quest         Quest      `json:"quest"`
	Completed     bool       `json:"completed"`    // ever completed (daily: today)
	CanComplete   bool       `json:"can_complete"` // eligible to complete now
	Completions   int        `json:"completions"`  // lifetime completion count
	LastCompleted *time.Time `json:"last_completed,omitempty"`
}

// QuestCompletionResult is returned by a successful complete: the turns granted
// and the player's resulting campaign turn balance.
type QuestCompletionResult struct {
	Completed    bool  `json:"completed"`
	TurnsGranted int64 `json:"turns_granted"`
	TurnBalance  int64 `json:"turn_balance"`
}

// IsRepeatable reports whether a quest type may be completed more than once
// (daily_checkin, once per day). All other types are one-shot per player.
func (q *Quest) IsRepeatable() bool {
	return q.Type == QuestDailyCheckin
}
