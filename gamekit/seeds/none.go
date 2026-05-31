// Package seeds holds the built-in SeedGenerator implementations.
package seeds

import (
	"context"

	"github.com/muse/gamekit"
	"github.com/muse/gamekit/types"
)

// None is the seed generator for games that need no server-issued seed
// (spin wheel, scratch card). It returns a nil seed.
type None struct{}

// Generate returns no seed data.
func (None) Generate(ctx context.Context, deps gamekit.HandlerDeps, game *types.Game, session *types.Session) (types.SeedData, error) {
	return nil, nil
}
