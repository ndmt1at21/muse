// Package handlers holds the built-in RewardHandler implementations.
package handlers

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// Probability is a weighted-random reward handler for spin wheel / scratch card.
// The server picks the outcome (anti-cheat #1: server-authoritative). The client
// payload is ignored. handler_config shape:
//
//	{ "prizes": [ { "prize_id": "p_001", "probability": 0.05, "slot_index": 0 },
//	              { "prize_id": "",      "probability": 0.95, "slot_index": 1 } ] }
//
// An empty prize_id is a "no-win" slot (no stock deduction). Weights need not
// sum to 1 — they are normalized. The chosen prize's live details (name/image/
// value/type) are loaded from the PrizeStore so the wheel config can't lie.
type Probability struct{}

type probConfig struct {
	Prizes []weightedEntry `json:"prizes"`
}

// Evaluate performs the weighted draw and returns the chosen reward.
func (Probability) Evaluate(ctx context.Context, deps gamekit.HandlerDeps, game *types.Game, seed types.SeedData, payload types.Payload) (types.RewardResult, error) {
	var cfg probConfig
	if err := json.Unmarshal(game.HandlerConfig, &cfg); err != nil {
		return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "invalid probability handler_config").Wrap(err)
	}
	if len(cfg.Prizes) == 0 {
		return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "probability handler_config has no prizes")
	}

	chosen, err := weightedPick(deps.Rand, cfg.Prizes)
	if err != nil {
		return types.RewardResult{}, err
	}

	meta := map[string]any{"slot_index": chosen.SlotIndex}

	// No-win slot: no prize, no stock.
	if chosen.PrizeID == "" {
		return types.RewardResult{Rewards: nil, Metadata: meta}, nil
	}

	// Load authoritative prize details (the engine deducts stock).
	reward, err := loadReward(ctx, deps, game.Scope, chosen.PrizeID)
	if err != nil {
		return types.RewardResult{}, err
	}
	return types.RewardResult{Rewards: []types.Reward{reward}, Metadata: meta}, nil
}
