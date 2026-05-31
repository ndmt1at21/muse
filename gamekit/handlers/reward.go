package handlers

import (
	"context"

	"github.com/muse/gamekit"
	"github.com/muse/gamekit/types"
)

// loadReward fetches a prize's authoritative details and builds a single-unit
// reward. The config only references prize_id; name/image/value/type always
// come from the live prize so the game config cannot misstate them. The engine
// performs the stock deduction (one per reward entry).
func loadReward(ctx context.Context, deps gamekit.HandlerDeps, scope types.Scope, prizeID string) (types.Reward, error) {
	prize, err := deps.Prizes.GetPrize(ctx, scope, prizeID)
	if err != nil {
		return types.Reward{}, err
	}
	return types.Reward{
		PrizeID:  prize.ID,
		Name:     prize.Name,
		Image:    prize.Image,
		Type:     prize.Type,
		Quantity: 1,
		Value:    prize.Value,
	}, nil
}
