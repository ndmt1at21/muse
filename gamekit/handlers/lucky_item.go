package handlers

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// LuckyItem is a collection-game spin handler: each play awards an intermediate
// "lucky item" (a wallet currency) by a weighted draw. The won item is credited
// to the player's wallet (the engine routes `lucky_item` rewards to the wallet
// ledger, not to fulfillment), and milestones convert accumulated items into
// real prizes. handler_config shape:
//
//	{ "items": [ { "item": "lucky_star", "weight": 5, "quantity": 1, "slot_index": 0 },
//	             { "item": "",           "weight": 5, "slot_index": 1 } ] }
//
// An empty item is a "no-win" slot. Weights need not sum to 1.
type LuckyItem struct{}

type luckyConfig struct {
	Items []luckyEntry `json:"items"`
}

type luckyEntry struct {
	Item      string  `json:"item"`
	Weight    float64 `json:"weight"`
	Quantity  int64   `json:"quantity"`
	SlotIndex int     `json:"slot_index"`
}

func (e luckyEntry) weight() float64 { return e.Weight }

// Evaluate performs the weighted draw and returns the won lucky item as a
// wallet-currency reward (Type=lucky_item, Name=item, Quantity).
func (LuckyItem) Evaluate(ctx context.Context, deps gamekit.HandlerDeps, game *types.Game, seed types.SeedData, payload types.Payload) (types.RewardResult, error) {
	var cfg luckyConfig
	if err := json.Unmarshal(game.HandlerConfig, &cfg); err != nil {
		return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "invalid lucky_item handler_config").Wrap(err)
	}
	if len(cfg.Items) == 0 {
		return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "lucky_item handler_config has no items")
	}

	weights := make([]float64, len(cfg.Items))
	for i, it := range cfg.Items {
		w := it.weight()
		if w < 0 {
			w = 0
		}
		weights[i] = w
	}
	idx := weightedIndex(deps.Rand, weights)
	chosen := cfg.Items[idx]
	meta := map[string]any{"slot_index": chosen.SlotIndex, "item": chosen.Item}

	if chosen.Item == "" { // no-win slot
		return types.RewardResult{Rewards: nil, Metadata: meta}, nil
	}
	qty := chosen.Quantity
	if qty <= 0 {
		qty = 1
	}
	return types.RewardResult{
		Rewards: []types.Reward{{
			Name:     chosen.Item,
			Type:     types.RewardLuckyItem,
			Quantity: qty,
		}},
		Metadata: meta,
	}, nil
}
