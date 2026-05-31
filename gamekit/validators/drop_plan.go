package validators

import (
	"context"
	"encoding/json"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// DropPlan is the gift-catcher anti-cheat validator. It checks the reported
// caught drop_ids against the server-generated sequence on the session: every
// id must exist, none may be claimed twice, and no drop type may be caught more
// than its max_catchable. The client cannot invent drops or over-claim.
//
// payload: { "caught_items": ["d_3","d_17", ...] }
type DropPlan struct{}

type dropPayload struct {
	CaughtItems []string `json:"caught_items"`
}

// Validate enforces existence, no-duplicates, and per-type catch caps.
func (DropPlan) Validate(ctx context.Context, game *types.Game, session *types.Session, payload types.Payload) error {
	if len(session.SeedData) == 0 {
		return gkerr.New(gkerr.ReasonCheatDetected, "no drop sequence on session")
	}
	var seq types.DropSequence
	if err := json.Unmarshal(session.SeedData, &seq); err != nil {
		return gkerr.New(gkerr.ReasonInternal, "decode drop sequence").Wrap(err)
	}
	var p dropPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &p); err != nil {
			return gkerr.New(gkerr.ReasonValidationFailed, "payload must contain caught_items").Wrap(err)
		}
	}

	idType := make(map[string]string, len(seq.Drops))
	for _, d := range seq.Drops {
		idType[d.ID] = d.Type
	}

	seen := make(map[string]struct{}, len(p.CaughtItems))
	perType := make(map[string]int)
	for _, id := range p.CaughtItems {
		typ, ok := idType[id]
		if !ok {
			return gkerr.New(gkerr.ReasonCheatDetected, "unknown drop id").WithMeta("drop_id", id)
		}
		if _, dup := seen[id]; dup {
			return gkerr.New(gkerr.ReasonCheatDetected, "duplicate caught drop").WithMeta("drop_id", id)
		}
		seen[id] = struct{}{}
		perType[typ]++
		if typ != types.EmptyDropType {
			if cap := seq.Plan[typ].MaxCatchable; cap > 0 && perType[typ] > cap {
				return gkerr.Newf(gkerr.ReasonCheatDetected, "caught %d of %q, exceeds cap %d", perType[typ], typ, cap).
					WithMeta("type", typ)
			}
		}
	}
	return nil
}
