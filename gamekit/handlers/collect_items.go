package handlers

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/muse/gamekit"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// CollectItems awards prizes for the items a player caught in a gift-catcher
// game. It trusts only the server-generated drop sequence on the session (not
// the client): it maps each caught drop_id to its type, caps the count at the
// type's max_catchable, and emits one reward per caught unit (so the engine
// deducts stock per unit). The drop_plan validator has already rejected unknown
// ids, duplicates, and over-claims before this runs.
//
// payload: { "caught_items": ["d_3","d_17", ...] }
type CollectItems struct{}

type collectPayload struct {
	CaughtItems []string `json:"caught_items"`
}

// Evaluate aggregates caught items by type and awards their mapped prizes.
func (CollectItems) Evaluate(ctx context.Context, deps gamekit.HandlerDeps, game *types.Game, seed types.SeedData, payload types.Payload) (types.RewardResult, error) {
	if len(seed) == 0 {
		return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "no drop sequence on session")
	}
	var seq types.DropSequence
	if err := json.Unmarshal(seed, &seq); err != nil {
		return types.RewardResult{}, gkerr.New(gkerr.ReasonInternal, "decode drop sequence").Wrap(err)
	}
	var p collectPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &p); err != nil {
			return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "payload must contain caught_items").Wrap(err)
		}
	}

	idType := make(map[string]string, len(seq.Drops))
	for _, d := range seq.Drops {
		idType[d.ID] = d.Type
	}

	// Count caught items per rewarding type, capped at max_catchable.
	counts := map[string]int{}
	seen := map[string]struct{}{}
	for _, id := range p.CaughtItems {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		typ, ok := idType[id]
		if !ok || typ == types.EmptyDropType {
			continue
		}
		entry := seq.Plan[typ]
		if entry.PrizeID == "" {
			continue
		}
		if entry.MaxCatchable > 0 && counts[typ] >= entry.MaxCatchable {
			continue
		}
		counts[typ]++
	}

	// Emit one reward per caught unit (stable order for deterministic output).
	var rewards []types.Reward
	byType := map[string]int{}
	total := 0
	for _, typ := range sortedKeys(counts) {
		n := counts[typ]
		reward, err := loadReward(ctx, deps, game.Scope, seq.Plan[typ].PrizeID)
		if err != nil {
			return types.RewardResult{}, err
		}
		for i := 0; i < n; i++ {
			rewards = append(rewards, reward)
		}
		byType[typ] = n
		total += n
	}

	meta := map[string]any{"total_caught": total, "by_type": byType}
	return types.RewardResult{Rewards: rewards, Metadata: meta}, nil
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
