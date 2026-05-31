package types

// DropSequence is the server-authoritative seed for collect-item games
// (gift-catcher). It is generated at Start by the drop_sequence seed generator,
// stored on the session, and is the single source of truth the collect_items
// handler awards from and the drop_plan validator checks against — so the
// client can neither invent drops nor exceed the per-type catch caps.
type DropSequence struct {
	Drops      []Drop                   `json:"drops"`       // the falling items, in order
	Plan       map[string]DropPlanEntry `json:"plan"`        // per drop type: prize + cap
	IntervalMS int64                    `json:"interval_ms"` // spacing between spawns
	TotalItems int                      `json:"total_items"`
}

// Drop is one falling item with a server-issued id and spawn offset.
type Drop struct {
	ID   string `json:"drop_id"`
	Type string `json:"type"`
	T    int64  `json:"t"` // spawn offset in ms from start
}

// DropPlanEntry maps a drop type to the prize it awards and how many of that
// type a single play may legitimately catch.
type DropPlanEntry struct {
	PrizeID      string `json:"prize_id"`
	MaxCatchable int    `json:"max_catchable"`
}

// EmptyDropType is the non-rewarding filler drop type.
const EmptyDropType = "empty"
