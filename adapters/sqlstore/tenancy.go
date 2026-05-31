package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/muse/gamekit/defaults"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

var tenantIDs defaults.IDGen

// --- TenantStore ---

type tenantRow struct {
	ID        string    `db:"id"`
	Name      string    `db:"name"`
	Plan      string    `db:"plan"`
	Settings  []byte    `db:"settings"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

func (r tenantRow) toDomain() types.Tenant {
	t := types.Tenant{ID: r.ID, Name: r.Name, Plan: r.Plan, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if len(r.Settings) > 0 {
		_ = json.Unmarshal(r.Settings, &t.Settings)
	}
	return t
}

// CreateTenant implements ports.TenantStore.
func (db *DB) CreateTenant(ctx context.Context, t *types.Tenant) (*types.Tenant, error) {
	if t.ID == "" {
		t.ID = tenantIDs.NewID("tenant")
	}
	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt = now, now
	settings, _ := json.Marshal(t.Settings)
	_, err := db.execContext(ctx,
		`INSERT INTO tenants (id, name, plan, settings, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		t.ID, t.Name, t.Plan, string(settings), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "insert tenant").Wrap(err)
	}
	return t, nil
}

// GetTenant implements ports.TenantStore.
func (db *DB) GetTenant(ctx context.Context, tenantID string) (*types.Tenant, error) {
	var row tenantRow
	err := db.getContext(ctx, &row,
		`SELECT id, name, plan, settings, created_at, updated_at FROM tenants WHERE id = ?`, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "tenant not found").WithMeta("tenant_id", tenantID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load tenant").Wrap(err)
	}
	t := row.toDomain()
	return &t, nil
}

// UpdateTenant implements ports.TenantStore (name/plan/settings).
func (db *DB) UpdateTenant(ctx context.Context, t *types.Tenant) (*types.Tenant, error) {
	settings, _ := json.Marshal(t.Settings)
	res, err := db.execContext(ctx,
		`UPDATE tenants SET name=?, plan=?, settings=?, updated_at=? WHERE id=?`,
		t.Name, t.Plan, string(settings), time.Now().UTC(), t.ID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "update tenant").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "tenant not found").WithMeta("tenant_id", t.ID)
	}
	return db.GetTenant(ctx, t.ID)
}

// ListTenants implements ports.TenantStore (newest-id last; cursor by created_at).
func (db *DB) ListTenants(ctx context.Context, limit int, cursor string) ([]types.Tenant, string, error) {
	limit = clampLimit(limit)
	where, args := "", []any{}
	if cursor != "" {
		if ts, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			where = ` WHERE created_at > ?`
			args = append(args, ts)
		}
	}
	args = append(args, limit+1)
	var rows []tenantRow
	if err := db.selectContext(ctx, &rows,
		`SELECT id, name, plan, settings, created_at, updated_at FROM tenants`+where+` ORDER BY created_at LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list tenants").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.Tenant, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, next, nil
}

// --- MerchantStore ---

type merchantRow struct {
	ID        string    `db:"id"`
	TenantID  string    `db:"tenant_id"`
	Name      string    `db:"name"`
	Logo      string    `db:"logo"`
	Settings  []byte    `db:"settings"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

func (r merchantRow) toDomain() types.Merchant {
	m := types.Merchant{ID: r.ID, TenantID: r.TenantID, Name: r.Name, Logo: r.Logo, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if len(r.Settings) > 0 {
		_ = json.Unmarshal(r.Settings, &m.Settings)
	}
	return m
}

// CreateMerchant implements ports.MerchantStore.
func (db *DB) CreateMerchant(ctx context.Context, m *types.Merchant) (*types.Merchant, error) {
	if m.ID == "" {
		m.ID = tenantIDs.NewID("merchant")
	}
	now := time.Now().UTC()
	m.CreatedAt, m.UpdatedAt = now, now
	settings, _ := json.Marshal(m.Settings)
	_, err := db.execContext(ctx,
		`INSERT INTO merchants (id, tenant_id, name, logo, settings, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		m.ID, m.TenantID, m.Name, m.Logo, string(settings), m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "insert merchant").Wrap(err)
	}
	return m, nil
}

// GetMerchant implements ports.MerchantStore (tenant-scoped).
func (db *DB) GetMerchant(ctx context.Context, tenantID, merchantID string) (*types.Merchant, error) {
	var row merchantRow
	err := db.getContext(ctx, &row,
		`SELECT id, tenant_id, name, logo, settings, created_at, updated_at FROM merchants WHERE id=? AND tenant_id=?`,
		merchantID, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "merchant not found").WithMeta("merchant_id", merchantID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load merchant").Wrap(err)
	}
	m := row.toDomain()
	return &m, nil
}

// UpdateMerchant implements ports.MerchantStore (tenant-scoped).
func (db *DB) UpdateMerchant(ctx context.Context, m *types.Merchant) (*types.Merchant, error) {
	settings, _ := json.Marshal(m.Settings)
	res, err := db.execContext(ctx,
		`UPDATE merchants SET name=?, logo=?, settings=?, updated_at=? WHERE id=? AND tenant_id=?`,
		m.Name, m.Logo, string(settings), time.Now().UTC(), m.ID, m.TenantID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "update merchant").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "merchant not found").WithMeta("merchant_id", m.ID)
	}
	return db.GetMerchant(ctx, m.TenantID, m.ID)
}

// ListMerchants implements ports.MerchantStore (tenant-scoped).
func (db *DB) ListMerchants(ctx context.Context, tenantID string, limit int, cursor string) ([]types.Merchant, string, error) {
	limit = clampLimit(limit)
	where := ` WHERE tenant_id=?`
	args := []any{tenantID}
	if cursor != "" {
		if ts, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			where += ` AND created_at > ?`
			args = append(args, ts)
		}
	}
	args = append(args, limit+1)
	var rows []merchantRow
	if err := db.selectContext(ctx, &rows,
		`SELECT id, tenant_id, name, logo, settings, created_at, updated_at FROM merchants`+where+` ORDER BY created_at LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list merchants").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.Merchant, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, next, nil
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}
