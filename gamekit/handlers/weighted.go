package handlers

import (
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/ports"
)

// weightedEntry is one candidate in a weighted draw. An empty PrizeID denotes a
// "no-win" outcome (the engine deducts no stock for it).
type weightedEntry struct {
	PrizeID   string  `json:"prize_id"`
	Weight    float64 `json:"probability"`
	SlotIndex int     `json:"slot_index"`
}

// weightedPick selects one entry with probability proportional to its weight.
// Weights need not sum to 1 (they are normalized). Returns an error for an
// empty set, a negative weight, or a non-positive total.
func weightedPick(rand ports.RandSource, entries []weightedEntry) (weightedEntry, error) {
	if len(entries) == 0 {
		return weightedEntry{}, gkerr.New(gkerr.ReasonValidationFailed, "empty prize set")
	}
	var total float64
	for _, e := range entries {
		if e.Weight < 0 {
			return weightedEntry{}, gkerr.New(gkerr.ReasonValidationFailed, "negative probability")
		}
		total += e.Weight
	}
	if total <= 0 {
		return weightedEntry{}, gkerr.New(gkerr.ReasonValidationFailed, "probabilities sum to zero")
	}
	r := rand.Float64() * total
	var acc float64
	for _, e := range entries {
		acc += e.Weight
		if r < acc {
			return e, nil
		}
	}
	return entries[len(entries)-1], nil // float rounding edge
}

// weightedIndex picks an index in [0,len(weights)) proportional to weight.
// Non-positive total falls back to index 0. Negative weights are treated as 0.
func weightedIndex(rand ports.RandSource, weights []float64) int {
	var total float64
	for _, w := range weights {
		if w > 0 {
			total += w
		}
	}
	if total <= 0 {
		return 0
	}
	r := rand.Float64() * total
	var acc float64
	for i, w := range weights {
		if w > 0 {
			acc += w
		}
		if r < acc {
			return i
		}
	}
	return len(weights) - 1
}
