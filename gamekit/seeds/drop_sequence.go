package seeds

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// DropSequence generates the server-authoritative falling-item sequence for a
// collect-item game (gift-catcher) at Start. The client receives it to render,
// catches some items, and reports the caught drop_ids — which the drop_plan
// validator checks and the collect_items handler awards from.
//
// handler_config:
//
//	{ "drops": [ {"type":"voucher_50k","prize_id":"p_v50","frequency":3,"max_catchable":2} ],
//	  "total_items": 40, "interval_ms": 500 }
//
// Each defined type contributes exactly `frequency` drops; the rest are filler
// "empty" drops up to total_items. The whole sequence is shuffled, then each
// drop gets a stable id ("d_<n>") and spawn offset.
type DropSequence struct{}

type dropDef struct {
	Type         string `json:"type"`
	PrizeID      string `json:"prize_id"`
	Frequency    int    `json:"frequency"`
	MaxCatchable int    `json:"max_catchable"`
}

type dropConfig struct {
	Drops      []dropDef `json:"drops"`
	TotalItems int       `json:"total_items"`
	IntervalMS int64     `json:"interval_ms"`
}

// Generate expands the config into a concrete, shuffled drop sequence.
func (DropSequence) Generate(ctx context.Context, deps gamekit.HandlerDeps, game *types.Game, session *types.Session) (types.SeedData, error) {
	var cfg dropConfig
	if err := json.Unmarshal(game.HandlerConfig, &cfg); err != nil {
		return nil, gkerr.New(gkerr.ReasonValidationFailed, "invalid drop_sequence handler_config").Wrap(err)
	}
	if cfg.IntervalMS <= 0 {
		cfg.IntervalMS = 500
	}

	plan := make(map[string]types.DropPlanEntry, len(cfg.Drops))
	var typed []string
	for _, d := range cfg.Drops {
		if d.Type == "" || d.Type == types.EmptyDropType {
			return nil, gkerr.New(gkerr.ReasonValidationFailed, "drop type must be non-empty and not 'empty'")
		}
		plan[d.Type] = types.DropPlanEntry{PrizeID: d.PrizeID, MaxCatchable: d.MaxCatchable}
		for i := 0; i < d.Frequency; i++ {
			typed = append(typed, d.Type)
		}
	}
	// Pad with filler up to total_items (typed count may already exceed it).
	total := cfg.TotalItems
	if total < len(typed) {
		total = len(typed)
	}
	kinds := make([]string, 0, total)
	kinds = append(kinds, typed...)
	for len(kinds) < total {
		kinds = append(kinds, types.EmptyDropType)
	}

	// Fisher–Yates shuffle via the injected RandSource (deterministic in tests).
	for i := len(kinds) - 1; i > 0; i-- {
		j := deps.Rand.Intn(i + 1)
		kinds[i], kinds[j] = kinds[j], kinds[i]
	}

	drops := make([]types.Drop, len(kinds))
	for i, k := range kinds {
		drops[i] = types.Drop{ID: "d_" + itoa(i), Type: k, T: int64(i) * cfg.IntervalMS}
	}

	seq := types.DropSequence{Drops: drops, Plan: plan, IntervalMS: cfg.IntervalMS, TotalItems: len(drops)}
	b, err := json.Marshal(seq)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "marshal drop sequence").Wrap(err)
	}
	return b, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
