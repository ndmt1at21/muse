// Package validators holds the built-in Validator implementations (anti-cheat).
package validators

import (
	"context"

	"github.com/muse/gamekit/types"
)

// Basic is the minimal validator: it trusts the engine's session checks
// (existence, not-consumed, not-expired, player/game binding) which already
// run before Validate is called. It performs no payload inspection — suitable
// for probability games where the server decides the outcome regardless of
// what the client sends.
type Basic struct{}

// Validate always passes; session integrity is enforced by the engine.
func (Basic) Validate(ctx context.Context, game *types.Game, session *types.Session, payload types.Payload) error {
	return nil
}
