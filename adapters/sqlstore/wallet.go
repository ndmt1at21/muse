package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
	"github.com/muse/pkg/dialect"
)

// --- WalletStore (Phase 8) ---
//
// wallet_balances is the derived per-currency balance (atomic upsert); wallet_
// ledger is the append-only audit of every movement. Credit/Spend update both
// inside the caller's transaction so a play's credit commits with the play.

// Credit implements ports.WalletStore: append a positive ledger entry and bump
// the balance atomically. Returns the new balance.
func (db *DB) Credit(ctx context.Context, tenantID, scopeKey, playerID, currency string, amount int64, reason, refID string) (int64, error) {
	if amount < 0 {
		return 0, gkerr.New(gkerr.ReasonValidationFailed, "credit amount must be non-negative")
	}
	if err := db.appendLedger(ctx, tenantID, scopeKey, playerID, currency, amount, reason, refID); err != nil {
		return 0, err
	}
	return db.bumpBalance(ctx, tenantID, scopeKey, playerID, currency, amount)
}

// Spend implements ports.WalletStore: conditionally debit iff balance >= amount.
// ok=false (no error) when insufficient.
func (db *DB) Spend(ctx context.Context, tenantID, scopeKey, playerID, currency string, amount int64, reason, refID string) (bool, int64, error) {
	if amount <= 0 {
		return false, 0, gkerr.New(gkerr.ReasonValidationFailed, "spend amount must be positive")
	}
	res, err := db.execContext(ctx,
		`UPDATE wallet_balances SET balance = balance - ?, updated_at = ?
		  WHERE tenant_id=? AND scope_key=? AND player_id=? AND currency=? AND balance >= ?`,
		amount, time.Now().UTC(), tenantID, scopeKey, playerID, currency, amount)
	if err != nil {
		return false, 0, gkerr.New(gkerr.ReasonInternal, "wallet spend").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		bal, _ := db.Balance(ctx, tenantID, scopeKey, playerID, currency)
		return false, bal, nil
	}
	if err := db.appendLedger(ctx, tenantID, scopeKey, playerID, currency, -amount, reason, refID); err != nil {
		return false, 0, err
	}
	bal, err := db.Balance(ctx, tenantID, scopeKey, playerID, currency)
	return true, bal, err
}

func (db *DB) appendLedger(ctx context.Context, tenantID, scopeKey, playerID, currency string, amount int64, reason, refID string) error {
	_, err := db.execContext(ctx,
		`INSERT INTO wallet_ledger (id, tenant_id, scope_key, player_id, currency, amount, reason, ref_id, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		tenantIDs.NewID("wl"), tenantID, scopeKey, playerID, currency, amount, reason, refID, time.Now().UTC())
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "append wallet ledger").Wrap(err)
	}
	return nil
}

func (db *DB) bumpBalance(ctx context.Context, tenantID, scopeKey, playerID, currency string, delta int64) (int64, error) {
	now := time.Now().UTC()
	var q string
	if db.kind == dialect.Postgres {
		q = `INSERT INTO wallet_balances (tenant_id, scope_key, player_id, currency, balance, updated_at)
		     VALUES (?,?,?,?,?,?)
		     ON CONFLICT (tenant_id, scope_key, player_id, currency)
		     DO UPDATE SET balance = wallet_balances.balance + ?, updated_at = ?`
	} else {
		q = `INSERT INTO wallet_balances (tenant_id, scope_key, player_id, currency, balance, updated_at)
		     VALUES (?,?,?,?,?,?)
		     ON DUPLICATE KEY UPDATE balance = balance + ?, updated_at = ?`
	}
	if _, err := db.execContext(ctx, q,
		tenantID, scopeKey, playerID, currency, delta, now, delta, now); err != nil {
		return 0, gkerr.New(gkerr.ReasonInternal, "bump wallet balance").Wrap(err)
	}
	return db.Balance(ctx, tenantID, scopeKey, playerID, currency)
}

// Balance implements ports.WalletStore (0 if none).
func (db *DB) Balance(ctx context.Context, tenantID, scopeKey, playerID, currency string) (int64, error) {
	var bal int64
	err := db.getContext(ctx, &bal,
		`SELECT balance FROM wallet_balances WHERE tenant_id=? AND scope_key=? AND player_id=? AND currency=?`,
		tenantID, scopeKey, playerID, currency)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, gkerr.New(gkerr.ReasonInternal, "get wallet balance").Wrap(err)
	}
	return bal, nil
}

// Balances implements ports.WalletStore (all currencies for a player+scope).
func (db *DB) Balances(ctx context.Context, tenantID, scopeKey, playerID string) (map[string]int64, error) {
	var rows []struct {
		Currency string `db:"currency"`
		Balance  int64  `db:"balance"`
	}
	if err := db.selectContext(ctx, &rows,
		`SELECT currency, balance FROM wallet_balances WHERE tenant_id=? AND scope_key=? AND player_id=?`,
		tenantID, scopeKey, playerID); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "list wallet balances").Wrap(err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Currency] = r.Balance
	}
	return out, nil
}

type ledgerRow struct {
	ID        string    `db:"id"`
	TenantID  string    `db:"tenant_id"`
	ScopeKey  string    `db:"scope_key"`
	PlayerID  string    `db:"player_id"`
	Currency  string    `db:"currency"`
	Amount    int64     `db:"amount"`
	Reason    string    `db:"reason"`
	RefID     string    `db:"ref_id"`
	CreatedAt time.Time `db:"created_at"`
}

// Ledger implements ports.WalletStore (newest-first, cursor by created_at desc).
func (db *DB) Ledger(ctx context.Context, tenantID, scopeKey, playerID string, limit int, cursor string) ([]types.LedgerEntry, string, error) {
	limit = clampLimit(limit)
	where := ` WHERE tenant_id=? AND scope_key=? AND player_id=?`
	args := []any{tenantID, scopeKey, playerID}
	if cursor != "" {
		if ts, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			where += ` AND created_at < ?`
			args = append(args, ts)
		}
	}
	args = append(args, limit+1)
	var rows []ledgerRow
	if err := db.selectContext(ctx, &rows,
		`SELECT id, tenant_id, scope_key, player_id, currency, amount, reason, ref_id, created_at
		   FROM wallet_ledger`+where+` ORDER BY created_at DESC LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list wallet ledger").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.LedgerEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.LedgerEntry{
			ID: r.ID, TenantID: r.TenantID, ScopeKey: r.ScopeKey, PlayerID: r.PlayerID,
			Currency: r.Currency, Amount: r.Amount, Reason: r.Reason, RefID: r.RefID, CreatedAt: r.CreatedAt,
		})
	}
	return out, next, nil
}

// GrantMilestoneOnce implements ports.WalletStore: insert the grant iff absent.
// granted=true means this call won the race (so the caller mints the prize).
func (db *DB) GrantMilestoneOnce(ctx context.Context, g *types.MilestoneGrant) (bool, error) {
	now := time.Now().UTC()
	var q string
	if db.kind == dialect.Postgres {
		q = `INSERT INTO milestone_grants (tenant_id, scope_key, player_id, milestone_id, created_at)
		     VALUES (?,?,?,?,?) ON CONFLICT (tenant_id, scope_key, player_id, milestone_id) DO NOTHING`
	} else {
		q = `INSERT IGNORE INTO milestone_grants (tenant_id, scope_key, player_id, milestone_id, created_at)
		     VALUES (?,?,?,?,?)`
	}
	res, err := db.execContext(ctx, q, g.TenantID, g.ScopeKey, g.PlayerID, g.MilestoneID, now)
	if err != nil {
		return false, gkerr.New(gkerr.ReasonInternal, "grant milestone").Wrap(err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GrantedMilestones implements ports.WalletStore (set of granted milestone ids).
func (db *DB) GrantedMilestones(ctx context.Context, tenantID, scopeKey, playerID string) (map[string]bool, error) {
	var ids []string
	if err := db.selectContext(ctx, &ids,
		`SELECT milestone_id FROM milestone_grants WHERE tenant_id=? AND scope_key=? AND player_id=?`,
		tenantID, scopeKey, playerID); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "list milestone grants").Wrap(err)
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
