// Package leaderboard holds the pure ranking logic: how a play updates a
// player's metric score and how a recurring window is keyed. It depends only on
// types so it is embeddable and unit-testable without a DB or Redis. Storage,
// the Redis sorted set, finalize, and anti-cheat actions live in the hosting
// layer (core) over the ports.
package leaderboard

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/muse/gamekit/types"
)

// MetricNumber pulls a numeric field out of handler metadata (score / points /
// collection counters), tolerating int or float JSON.
func MetricNumber(metadata map[string]any, key string) int64 {
	if metadata == nil {
		return 0
	}
	switch v := metadata[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

// PrizeWins counts the prize-winning rewards in a play (for total_wins).
func PrizeWins(rewards []types.Reward) int64 {
	var n int64
	for _, r := range rewards {
		if r.PrizeID != "" {
			n++
		}
	}
	return n
}

// ApplyMetric returns a player's new score for a metric after one play, given
// their current score. high_score takes the max; the others accumulate.
func ApplyMetric(metric string, current int64, rewards []types.Reward, metadata map[string]any) int64 {
	switch metric {
	case types.MetricHighScore:
		if s := MetricNumber(metadata, "score"); s > current {
			return s
		}
		return current
	case types.MetricTotalScore:
		return current + MetricNumber(metadata, "score")
	case types.MetricTotalWins:
		return current + PrizeWins(rewards)
	case types.MetricTotalPlays:
		return current + 1
	case types.MetricQuestPoints:
		return current + MetricNumber(metadata, "points")
	case types.MetricCollectionCount:
		return current + MetricNumber(metadata, "collected")
	default:
		return current
	}
}

// Contribution is how one play feeds a metric's score: the per-play value and
// whether it competes as a maximum (high_score) rather than accumulating. The
// store applies it atomically (GREATEST vs += ) so concurrent plays are safe.
func Contribution(metric string, rewards []types.Reward, metadata map[string]any) (value int64, isMax bool) {
	switch metric {
	case types.MetricHighScore:
		return MetricNumber(metadata, "score"), true
	case types.MetricTotalScore:
		return MetricNumber(metadata, "score"), false
	case types.MetricTotalWins:
		return PrizeWins(rewards), false
	case types.MetricTotalPlays:
		return 1, false
	case types.MetricQuestPoints:
		return MetricNumber(metadata, "points"), false
	case types.MetricCollectionCount:
		return MetricNumber(metadata, "collected"), false
	default:
		return 0, false
	}
}

// WindowKey is the stable key for the window a timestamp falls in. Recurring
// windows roll over by period (so a new daily/weekly/monthly board starts
// automatically); fixed/manual share a single key for the whole config.
func WindowKey(w types.TimeWindow, now time.Time) string {
	if w.Type != types.WindowRecurring {
		return w.Type // "fixed" | "manual"
	}
	u := now.UTC()
	switch w.Period {
	case types.PeriodDaily:
		return u.Format("2006-01-02")
	case types.PeriodWeekly:
		y, wk := u.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, wk)
	case types.PeriodMonthly:
		return u.Format("2006-01")
	default:
		return types.WindowRecurring
	}
}

// IsOpen reports whether the board accepts updates at `now`: fixed/manual windows
// honor their Start/End bounds; recurring windows are always open (they roll).
func IsOpen(w types.TimeWindow, now time.Time) bool {
	if w.Type == types.WindowRecurring {
		return true
	}
	if w.Start != nil && now.Before(*w.Start) {
		return false
	}
	if w.End != nil && now.After(*w.End) {
		return false
	}
	return true
}
