package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

type sessionRow struct {
	ID         string    `db:"id"`
	TenantID   string    `db:"tenant_id"`
	MerchantID string    `db:"merchant_id"`
	GameID     string    `db:"game_id"`
	PlayerID   string    `db:"player_id"`
	SeedData   []byte    `db:"seed_data"`
	Secret     string    `db:"secret"`
	StartedAt  time.Time `db:"started_at"`
	ExpiresAt  time.Time `db:"expires_at"`
	Consumed   bool      `db:"consumed"`
}

// CreateSession implements ports.SessionStore. The durable session row doubles
// as the audit trail; Redis caching can front this later (Phase 9).
func (db *DB) CreateSession(ctx context.Context, s *types.Session) error {
	var seed any
	if len(s.SeedData) > 0 {
		seed = string(s.SeedData)
	}
	_, err := db.execContext(ctx,
		`INSERT INTO sessions (id, tenant_id, merchant_id, game_id, player_id, seed_data, secret, started_at, expires_at, consumed)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Scope.TenantID, s.Scope.MerchantID, s.GameID, s.PlayerID, seed, s.Secret, s.StartedAt, s.ExpiresAt, false)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "create session").Wrap(err)
	}
	return nil
}

// GetSession implements ports.SessionStore.
func (db *DB) GetSession(ctx context.Context, scope types.Scope, sessionID string) (*types.Session, error) {
	var row sessionRow
	err := db.getContext(ctx, &row,
		`SELECT id, tenant_id, merchant_id, game_id, player_id, seed_data, secret, started_at, expires_at, consumed
		   FROM sessions WHERE id = ? AND tenant_id = ? AND merchant_id = ?`,
		sessionID, scope.TenantID, scope.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonSessionInvalid, "session not found").WithMeta("session_id", sessionID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load session").Wrap(err)
	}
	return &types.Session{
		ID:        row.ID,
		Scope:     types.Scope{TenantID: row.TenantID, MerchantID: row.MerchantID},
		GameID:    row.GameID,
		PlayerID:  row.PlayerID,
		SeedData:  json.RawMessage(row.SeedData),
		Secret:    row.Secret,
		StartedAt: row.StartedAt,
		ExpiresAt: row.ExpiresAt,
		Consumed:  row.Consumed,
	}, nil
}

// ConsumeSession atomically flips consumed false→true. RowsAffected == 0 means
// it was already consumed (or gone) — a single-use guarantee even under races,
// and it runs inside the Play transaction so a rollback un-consumes it.
func (db *DB) ConsumeSession(ctx context.Context, scope types.Scope, sessionID string) error {
	res, err := db.execContext(ctx,
		`UPDATE sessions SET consumed = ? WHERE id = ? AND tenant_id = ? AND merchant_id = ? AND consumed = ?`,
		true, sessionID, scope.TenantID, scope.MerchantID, false)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "consume session").Wrap(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "consume rows").Wrap(err)
	}
	if n == 0 {
		return gkerr.New(gkerr.ReasonSessionConsumed, "session already used").WithMeta("session_id", sessionID)
	}
	return nil
}
