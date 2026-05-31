package memstore

import (
	"context"

	"github.com/muse/gamekit/types"
)

// --- FulfillmentStore (in-memory) ---

// EnqueueTask implements ports.FulfillmentStore: it appends an outbox task. In
// the real adapter this INSERT runs inside the Play transaction; here the Store
// mutex provides the same atomicity for tests.
func (s *Store) EnqueueTask(ctx context.Context, t *types.FulfillmentTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, *t)
	return nil
}

// Tasks returns a snapshot of all enqueued outbox tasks (test helper).
func (s *Store) Tasks() []types.FulfillmentTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]types.FulfillmentTask, len(s.tasks))
	copy(out, s.tasks)
	return out
}
