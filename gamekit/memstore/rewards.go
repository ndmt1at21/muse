package memstore

import (
	"context"
	"time"

	"github.com/muse/gamekit/types"
)

// --- RewardStore (in-memory) ---

// PutCodes seeds the available voucher-code pool for a prize (test/example helper).
func (s *Store) PutCodes(scope types.Scope, prizeID string, codes ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(scope, prizeID)
	s.codes[k] = append(s.codes[k], codes...)
}

// Rewards returns a snapshot of all reward records (test helper).
func (s *Store) Rewards() []types.RewardRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]types.RewardRecord, len(s.rewards))
	copy(out, s.rewards)
	return out
}

// InsertReward implements ports.RewardStore.
func (s *Store) InsertReward(ctx context.Context, r *types.RewardRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rewards = append(s.rewards, *r)
	return nil
}

// CountAwards implements ports.RewardStore (non-revoked rewards of a prize).
func (s *Store) CountAwards(ctx context.Context, scope types.Scope, prizeID, playerID string, since time.Time) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total, today int
	for _, r := range s.rewards {
		if r.Scope == scope && r.PrizeID == prizeID && r.PlayerID == playerID && r.Status != types.RewardRevoked {
			total++
			if !r.CreatedAt.Before(since) {
				today++
			}
		}
	}
	return total, today, nil
}

// PopCode implements ports.RewardStore (atomic under the store mutex).
func (s *Store) PopCode(ctx context.Context, scope types.Scope, prizeID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(scope, prizeID)
	pool := s.codes[k]
	if len(pool) == 0 {
		return "", false, nil
	}
	code := pool[0]
	s.codes[k] = pool[1:]
	return code, true, nil
}
