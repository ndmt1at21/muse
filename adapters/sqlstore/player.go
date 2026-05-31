package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
	"github.com/muse/pkg/dialect"
)

// --- PlayerStore ---

type playerRow struct {
	ID         string    `db:"id"`
	TenantID   string    `db:"tenant_id"`
	MerchantID string    `db:"merchant_id"`
	IdentityID string    `db:"identity_id"`
	Profile    []byte    `db:"profile"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

func (r playerRow) toDomain() *types.Player {
	p := &types.Player{
		ID: r.ID, TenantID: r.TenantID, MerchantID: r.MerchantID, IdentityID: r.IdentityID,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if len(r.Profile) > 0 {
		_ = json.Unmarshal(r.Profile, &p.Profile)
	}
	return p
}

const playerCols = `id, tenant_id, merchant_id, identity_id, profile, created_at, updated_at`

// UpsertPlayer returns the existing (tenant, identity) player or inserts one.
// The UNIQUE(tenant_id, identity_id) constraint makes this race-safe: on a
// duplicate insert we fall back to the existing row.
func (db *DB) UpsertPlayer(ctx context.Context, p *types.Player) (*types.Player, bool, error) {
	if existing, err := db.GetPlayerByIdentity(ctx, p.TenantID, p.IdentityID); err == nil {
		return existing, false, nil
	} else if gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
		return nil, false, err
	}
	if p.ID == "" {
		p.ID = tenantIDs.NewID("player")
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	profile := nonEmptyJSON(toRawJSON(p.Profile))
	_, err := db.execContext(ctx,
		`INSERT INTO players (`+playerCols+`) VALUES (?,?,?,?,?,?,?)`,
		p.ID, p.TenantID, p.MerchantID, p.IdentityID, string(profile), p.CreatedAt, p.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			// Lost the race; return the winner.
			if existing, gErr := db.GetPlayerByIdentity(ctx, p.TenantID, p.IdentityID); gErr == nil {
				return existing, false, nil
			}
		}
		return nil, false, gkerr.New(gkerr.ReasonInternal, "insert player").Wrap(err)
	}
	return p, true, nil
}

// GetPlayer loads a player by id within a tenant.
func (db *DB) GetPlayer(ctx context.Context, tenantID, playerID string) (*types.Player, error) {
	var row playerRow
	err := db.getContext(ctx, &row,
		`SELECT `+playerCols+` FROM players WHERE id=? AND tenant_id=?`, playerID, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "player not found").WithMeta("player_id", playerID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load player").Wrap(err)
	}
	return row.toDomain(), nil
}

// GetPlayerByIdentity loads the tenant's player for an identity.
func (db *DB) GetPlayerByIdentity(ctx context.Context, tenantID, identityID string) (*types.Player, error) {
	var row playerRow
	err := db.getContext(ctx, &row,
		`SELECT `+playerCols+` FROM players WHERE tenant_id=? AND identity_id=?`, tenantID, identityID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "player not found")
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load player by identity").Wrap(err)
	}
	return row.toDomain(), nil
}

// UpdatePlayerProfile overwrites the tenant-scoped profile blob.
func (db *DB) UpdatePlayerProfile(ctx context.Context, tenantID, playerID string, profile map[string]any) (*types.Player, error) {
	blob := nonEmptyJSON(toRawJSON(profile))
	res, err := db.execContext(ctx,
		`UPDATE players SET profile=?, updated_at=? WHERE id=? AND tenant_id=?`,
		string(blob), time.Now().UTC(), playerID, tenantID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "update profile").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "player not found").WithMeta("player_id", playerID)
	}
	return db.GetPlayer(ctx, tenantID, playerID)
}

// --- turn balances ---

// GetTurnBalance returns the player's balance for a scope key (0 if none).
func (db *DB) GetTurnBalance(ctx context.Context, tenantID, playerID, scopeKey string) (int64, error) {
	var bal int64
	err := db.getContext(ctx, &bal,
		`SELECT balance FROM turn_balances WHERE tenant_id=? AND player_id=? AND scope_key=?`,
		tenantID, playerID, scopeKey)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, gkerr.New(gkerr.ReasonInternal, "get turn balance").Wrap(err)
	}
	return bal, nil
}

// GrantTurns atomically adds delta via an upsert, clamping at zero. Uses each
// engine's native upsert so concurrent grants don't lose updates.
func (db *DB) GrantTurns(ctx context.Context, tenantID, playerID, scopeKey string, delta int64) (int64, error) {
	now := time.Now().UTC()
	var q string
	if db.kind == dialect.Postgres {
		q = `INSERT INTO turn_balances (tenant_id, player_id, scope_key, balance, updated_at)
		     VALUES (?,?,?,GREATEST(?,0),?)
		     ON CONFLICT (tenant_id, player_id, scope_key)
		     DO UPDATE SET balance = GREATEST(turn_balances.balance + ?, 0), updated_at = ?`
	} else {
		q = `INSERT INTO turn_balances (tenant_id, player_id, scope_key, balance, updated_at)
		     VALUES (?,?,?,GREATEST(?,0),?)
		     ON DUPLICATE KEY UPDATE balance = GREATEST(balance + ?, 0), updated_at = ?`
	}
	if _, err := db.execContext(ctx, q,
		tenantID, playerID, scopeKey, delta, now, delta, now); err != nil {
		return 0, gkerr.New(gkerr.ReasonInternal, "grant turns").Wrap(err)
	}
	return db.GetTurnBalance(ctx, tenantID, playerID, scopeKey)
}

// ConsumeTurn atomically decrements by 1 iff balance > 0 (single conditional
// UPDATE, race-safe on both engines).
func (db *DB) ConsumeTurn(ctx context.Context, tenantID, playerID, scopeKey string) (bool, int64, error) {
	res, err := db.execContext(ctx,
		`UPDATE turn_balances SET balance = balance - 1, updated_at = ?
		  WHERE tenant_id=? AND player_id=? AND scope_key=? AND balance > 0`,
		time.Now().UTC(), tenantID, playerID, scopeKey)
	if err != nil {
		return false, 0, gkerr.New(gkerr.ReasonInternal, "consume turn").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, 0, nil
	}
	bal, err := db.GetTurnBalance(ctx, tenantID, playerID, scopeKey)
	return true, bal, err
}

// toRawJSON marshals a profile map to raw JSON (nil → empty).
func toRawJSON(m map[string]any) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}
