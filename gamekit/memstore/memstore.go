// Package memstore provides thread-safe, in-memory implementations of every
// gamekit port. It backs the SDK unit tests and the examples/embed program,
// proving the engine runs standalone with no DB/Redis (Mode A). It is NOT for
// production — state is lost on restart and there is no real durability.
package memstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

func key(scope types.Scope, id string) string {
	return scope.TenantID + "|" + scope.MerchantID + "|" + id
}

// Store is an in-memory implementation of all gamekit data ports.
type Store struct {
	mu       sync.Mutex
	games    map[string]*types.Game
	prizes   map[string]*types.Prize
	sessions map[string]*types.Session
	history  []types.PlayHistory
	rewards  []types.RewardRecord
	tasks    []types.FulfillmentTask
	codes    map[string][]string // scope|prizeID -> available codes

	// Tenancy & identity (Phase 4).
	tenants           map[string]*types.Tenant
	merchants         map[string]*types.Merchant // tenantID|merchantID
	identities        map[string]*types.Identity
	contacts          map[string]string // "type:value" -> identityID (global unique)
	players           map[string]*types.Player
	playersByIdentity map[string]string // tenantID|identityID -> playerID
	turns             map[string]int64  // tenantID|playerID|scopeKey -> balance
}

// New returns an empty store.
func New() *Store {
	return &Store{
		games:    map[string]*types.Game{},
		prizes:   map[string]*types.Prize{},
		sessions: map[string]*types.Session{},
		codes:    map[string][]string{},

		tenants:           map[string]*types.Tenant{},
		merchants:         map[string]*types.Merchant{},
		identities:        map[string]*types.Identity{},
		contacts:          map[string]string{},
		players:           map[string]*types.Player{},
		playersByIdentity: map[string]string{},
		turns:             map[string]int64{},
	}
}

// PutGame seeds a game (test/example helper).
func (s *Store) PutGame(g *types.Game) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.games[key(g.Scope, g.ID)] = g
}

// PutPrize seeds a prize (test/example helper).
func (s *Store) PutPrize(p *types.Prize) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prizes[key(p.Scope, p.ID)] = p
}

// --- GameStore ---

func (s *Store) GetGame(ctx context.Context, scope types.Scope, gameID string) (*types.Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[key(scope, gameID)]
	if !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "game not found").WithMeta("game_id", gameID)
	}
	cp := *g
	return &cp, nil
}

// --- PrizeStore ---

func (s *Store) GetPrize(ctx context.Context, scope types.Scope, prizeID string) (*types.Prize, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prizes[key(scope, prizeID)]
	if !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "prize not found").WithMeta("prize_id", prizeID)
	}
	cp := *p
	return &cp, nil
}

func (s *Store) ListPrizes(ctx context.Context, scope types.Scope, gameID string) ([]types.Prize, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []types.Prize
	for _, p := range s.prizes {
		if p.Scope == scope {
			out = append(out, *p)
		}
	}
	return out, nil
}

// Deduct atomically decrements remaining stock iff > 0 (mirrors the SQL
// conditional UPDATE). The mutex makes it safe under the concurrency test.
func (s *Store) Deduct(ctx context.Context, scope types.Scope, prizeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prizes[key(scope, prizeID)]
	if !ok {
		return false, gkerr.New(gkerr.ReasonNotFound, "prize not found").WithMeta("prize_id", prizeID)
	}
	if p.Remaining <= 0 {
		return false, nil
	}
	p.Remaining--
	return true, nil
}

// --- SessionStore ---

func (s *Store) CreateSession(ctx context.Context, sess *types.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *sess
	s.sessions[key(sess.Scope, sess.ID)] = &cp
	return nil
}

func (s *Store) GetSession(ctx context.Context, scope types.Scope, sessionID string) (*types.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[key(scope, sessionID)]
	if !ok {
		return nil, gkerr.New(gkerr.ReasonSessionInvalid, "session not found").WithMeta("session_id", sessionID)
	}
	cp := *sess
	return &cp, nil
}

func (s *Store) ConsumeSession(ctx context.Context, scope types.Scope, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[key(scope, sessionID)]
	if !ok {
		return gkerr.New(gkerr.ReasonSessionInvalid, "session not found").WithMeta("session_id", sessionID)
	}
	if sess.Consumed {
		return gkerr.New(gkerr.ReasonSessionConsumed, "session already used")
	}
	sess.Consumed = true
	return nil
}

// --- HistoryStore ---

func (s *Store) InsertHistory(ctx context.Context, h *types.PlayHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, *h)
	return nil
}

func (s *Store) ListHistory(ctx context.Context, scope types.Scope, gameID, playerID string, limit int, cursor string) ([]types.PlayHistory, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []types.PlayHistory
	for _, h := range s.history {
		if h.Scope == scope && h.GameID == gameID && h.PlayerID == playerID {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

func (s *Store) CountPlays(ctx context.Context, scope types.Scope, gameID, playerID string, since time.Time) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total, today int
	for _, h := range s.history {
		if h.Scope == scope && h.GameID == gameID && h.PlayerID == playerID {
			total++
			if !h.CreatedAt.Before(since) {
				today++
			}
		}
	}
	return total, today, nil
}
