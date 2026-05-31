package types

import "time"

// --- Leaderboard (Phase 7) ---
//
// A Leaderboard ranks players under a campaign by a metric, over a time window.
// The durable per-player standing lives in the DB (the source of truth);
// real-time rank/around-me/my-rank reads are served from a Redis sorted set the
// engine keeps in sync on each Play. finalize locks the window, snapshots the
// ranking, and batch-awards the configured prize tiers. Anti-cheat flags
// outliers; finalize only awards non-flagged, non-disqualified entries.

// Leaderboard metric types — what a higher number means "better".
const (
	MetricHighScore       = "high_score"       // best single-play score
	MetricTotalScore      = "total_score"      // sum of play scores
	MetricTotalWins       = "total_wins"       // count of prize-winning plays
	MetricTotalPlays      = "total_plays"      // count of plays
	MetricQuestPoints     = "quest_points"     // accumulated quest points (metadata)
	MetricCollectionCount = "collection_count" // distinct items collected (metadata)
)

// Time-window types and recurring periods.
const (
	WindowFixed     = "fixed"
	WindowRecurring = "recurring"
	WindowManual    = "manual"

	PeriodDaily   = "daily"
	PeriodWeekly  = "weekly"
	PeriodMonthly = "monthly"
)

// Leaderboard lifecycle + per-entry anti-cheat states.
const (
	LeaderboardActive    = "active"
	LeaderboardFinalized = "finalized"

	EntryActive       = "active"
	EntryFlagged      = "flagged"      // anti-cheat: excluded from awards pending review
	EntryDisqualified = "disqualified" // admin action: excluded + hidden
)

// TimeWindow bounds a leaderboard's competition. Recurring windows roll over by
// Period (the engine keys entries by the current window); fixed/manual use the
// explicit Start/End.
type TimeWindow struct {
	Type   string     `json:"type"`             // fixed | recurring | manual
	Period string     `json:"period,omitempty"` // daily | weekly | monthly (recurring)
	Start  *time.Time `json:"start,omitempty"`
	End    *time.Time `json:"end,omitempty"`
}

// PrizeTier awards a rank range a prize at finalize.
type PrizeTier struct {
	FromRank int    `json:"from_rank"` // 1-based, inclusive
	ToRank   int    `json:"to_rank"`   // inclusive
	PrizeID  string `json:"prize_id"`
}

// AntiCheat configures the leaderboard's outlier handling.
type AntiCheat struct {
	ScoreCeiling int64 `json:"score_ceiling,omitempty"` // scores above this are flagged (0 = off)
	FlagOutliers bool  `json:"flag_outliers,omitempty"`
}

// Leaderboard is the admin-configured ranking under a campaign.
type Leaderboard struct {
	ID         string      `json:"id"`
	TenantID   string      `json:"tenant_id"`
	MerchantID string      `json:"merchant_id"`
	CampaignID string      `json:"campaign_id"`
	Name       string      `json:"name"`
	Metric     string      `json:"metric"`
	Window     TimeWindow  `json:"time_window"`
	PrizeTiers []PrizeTier `json:"prize_tiers,omitempty"`
	AntiCheat  AntiCheat   `json:"anti_cheat"`
	Status     string      `json:"status"` // active | finalized
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// LeaderboardEntry is a player's durable standing in one window of a board.
type LeaderboardEntry struct {
	TenantID      string    `json:"tenant_id"`
	LeaderboardID string    `json:"leaderboard_id"`
	WindowKey     string    `json:"window_key"`
	PlayerID      string    `json:"player_id"`
	Score         int64     `json:"score"`
	Plays         int64     `json:"plays"`
	State         string    `json:"state"` // active | flagged | disqualified
	UpdatedAt     time.Time `json:"updated_at"`
}

// RankedEntry is an entry plus its 1-based rank (for rankings / around-me).
type RankedEntry struct {
	LeaderboardEntry
	Rank int64 `json:"rank"`
}
