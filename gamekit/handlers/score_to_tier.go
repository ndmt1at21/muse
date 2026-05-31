package handlers

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// ScoreToTier maps a reported score to a tier, then runs a weighted draw within
// that tier's prize group (egg-catcher). The score is the only client input;
// the server still picks the prize (anti-cheat: server-authoritative outcome).
// A score below all tiers, or an empty/no-win group, yields no reward.
//
// handler_config:
//
//	{ "tiers": [ {"min":0,"max":29,"prize_group":"t0"},
//	             {"min":30,"max":69,"prize_group":"t1"} ],
//	  "prize_groups": {
//	    "t1": [ {"prize_id":"p_voucher","probability":0.7},
//	            {"prize_id":"","probability":0.3} ] } }
type ScoreToTier struct{}

type tierDef struct {
	Min        int    `json:"min"`
	Max        int    `json:"max"`
	PrizeGroup string `json:"prize_group"`
}

type scoreConfig struct {
	Tiers       []tierDef                  `json:"tiers"`
	PrizeGroups map[string][]weightedEntry `json:"prize_groups"`
}

type scorePayload struct {
	Score int `json:"score"`
}

// Evaluate resolves the tier for payload.score and draws within its group.
func (ScoreToTier) Evaluate(ctx context.Context, deps gamekit.HandlerDeps, game *types.Game, seed types.SeedData, payload types.Payload) (types.RewardResult, error) {
	var cfg scoreConfig
	if err := json.Unmarshal(game.HandlerConfig, &cfg); err != nil {
		return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "invalid score_to_tier handler_config").Wrap(err)
	}
	var p scorePayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &p); err != nil {
			return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "payload must contain a numeric score").Wrap(err)
		}
	}

	tier := matchTier(cfg.Tiers, p.Score)
	meta := map[string]any{"score": p.Score, "tier": tier}

	if tier == "" {
		return types.RewardResult{Metadata: meta}, nil
	}
	group := cfg.PrizeGroups[tier]
	if len(group) == 0 {
		return types.RewardResult{Metadata: meta}, nil
	}

	chosen, err := weightedPick(deps.Rand, group)
	if err != nil {
		return types.RewardResult{}, err
	}
	if chosen.PrizeID == "" {
		return types.RewardResult{Metadata: meta}, nil
	}
	reward, err := loadReward(ctx, deps, game.Scope, chosen.PrizeID)
	if err != nil {
		return types.RewardResult{}, err
	}
	return types.RewardResult{Rewards: []types.Reward{reward}, Metadata: meta}, nil
}

// matchTier returns the prize_group of the first tier whose [min,max] contains
// score, or "" if none.
func matchTier(tiers []tierDef, score int) string {
	for _, t := range tiers {
		if score >= t.Min && score <= t.Max {
			return t.PrizeGroup
		}
	}
	return ""
}
