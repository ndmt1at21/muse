package validators

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// TimeAndScoreRange rejects implausible egg-catcher submissions: a score above
// the theoretical ceiling, a negative score, or a play whose reported duration
// is too short (bot) or too long (stale tab) for the game window. Reads its
// bounds from the game's validator_config:
//
//	{ "min_duration_ms": 3000, "max_duration_ms": 120000, "max_score": 150 }
//
// All bounds are optional; a zero/absent bound disables that check.
type TimeAndScoreRange struct{}

type tsrConfig struct {
	MinDurationMS int64 `json:"min_duration_ms"`
	MaxDurationMS int64 `json:"max_duration_ms"`
	MaxScore      int   `json:"max_score"`
}

type tsrPayload struct {
	Score      int   `json:"score"`
	DurationMS int64 `json:"duration_ms"`
}

// Validate enforces the configured score/duration bounds.
func (TimeAndScoreRange) Validate(ctx context.Context, game *types.Game, session *types.Session, payload types.Payload) error {
	var cfg tsrConfig
	if len(game.ValidatorConfig) > 0 {
		if err := json.Unmarshal(game.ValidatorConfig, &cfg); err != nil {
			return gkerr.New(gkerr.ReasonValidationFailed, "invalid time_and_score_range validator_config").Wrap(err)
		}
	}
	var p tsrPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &p); err != nil {
			return gkerr.New(gkerr.ReasonValidationFailed, "payload must contain score/duration_ms").Wrap(err)
		}
	}

	if p.Score < 0 {
		return gkerr.New(gkerr.ReasonCheatDetected, "negative score").WithMeta("score", p.Score)
	}
	if cfg.MaxScore > 0 && p.Score > cfg.MaxScore {
		return gkerr.Newf(gkerr.ReasonCheatDetected, "score %d exceeds ceiling %d", p.Score, cfg.MaxScore).
			WithMeta("score", p.Score).WithMeta("max_score", cfg.MaxScore)
	}
	// When a minimum duration is configured it is mandatory: a missing, zero, or
	// negative duration_ms is treated as too fast rather than skipping the check
	// (otherwise a bot bypasses the anti-cheat by simply omitting the field).
	if cfg.MinDurationMS > 0 && p.DurationMS < cfg.MinDurationMS {
		return gkerr.Newf(gkerr.ReasonCheatDetected, "play too fast: %dms < %dms", p.DurationMS, cfg.MinDurationMS).
			WithMeta("duration_ms", p.DurationMS)
	}
	if cfg.MaxDurationMS > 0 && p.DurationMS > cfg.MaxDurationMS {
		return gkerr.Newf(gkerr.ReasonCheatDetected, "play too slow: %dms > %dms", p.DurationMS, cfg.MaxDurationMS).
			WithMeta("duration_ms", p.DurationMS)
	}
	return nil
}
