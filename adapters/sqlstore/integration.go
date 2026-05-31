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

// --- Integration storage (Phase 10; campaign-optional, merchant-scoped) ---

const integrationCols = `id, tenant_id, merchant_id, campaign_id, type, events, config, status, created_at`

type integrationRow struct {
	ID         string    `db:"id"`
	TenantID   string    `db:"tenant_id"`
	MerchantID string    `db:"merchant_id"`
	CampaignID string    `db:"campaign_id"`
	Type       string    `db:"type"`
	Events     []byte    `db:"events"`
	Config     []byte    `db:"config"`
	Status     string    `db:"status"`
	CreatedAt  time.Time `db:"created_at"`
}

func (r integrationRow) toDomain() types.Integration {
	i := types.Integration{
		ID: r.ID, TenantID: r.TenantID, MerchantID: r.MerchantID, CampaignID: r.CampaignID,
		Type: r.Type, Status: r.Status, CreatedAt: r.CreatedAt,
	}
	if len(r.Events) > 0 {
		_ = json.Unmarshal(r.Events, &i.Events)
	}
	if len(r.Config) > 0 {
		i.Config = json.RawMessage(r.Config)
	}
	return i
}

// CreateIntegration inserts a new integration.
func (db *DB) CreateIntegration(ctx context.Context, i *types.Integration) (*types.Integration, error) {
	if i.ID == "" {
		i.ID = tenantIDs.NewID("intg")
	}
	if i.Status == "" {
		i.Status = types.IntegrationActive
	}
	i.CreatedAt = time.Now().UTC()
	events, _ := json.Marshal(i.Events)
	_, err := db.execContext(ctx,
		`INSERT INTO integrations (`+integrationCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		i.ID, i.TenantID, i.MerchantID, i.CampaignID, i.Type,
		string(events), string(nonEmptyJSON(i.Config)), i.Status, i.CreatedAt)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "insert integration").Wrap(err)
	}
	return i, nil
}

// DeleteIntegration removes an integration (merchant-scoped).
func (db *DB) DeleteIntegration(ctx context.Context, tenantID, merchantID, id string) error {
	res, err := db.execContext(ctx,
		`DELETE FROM integrations WHERE id=? AND tenant_id=? AND merchant_id=?`, id, tenantID, merchantID)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "delete integration").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gkerr.New(gkerr.ReasonNotFound, "integration not found").WithMeta("integration_id", id)
	}
	return nil
}

// ListIntegrations lists integrations under the merchant (optionally filtered by
// campaign), paginated by created_at.
func (db *DB) ListIntegrations(ctx context.Context, tenantID, merchantID, campaignID string, limit int, cursor string) ([]types.Integration, string, error) {
	limit = clampLimit(limit)
	where := ` WHERE tenant_id=? AND merchant_id=?`
	args := []any{tenantID, merchantID}
	if campaignID != "" {
		where += ` AND campaign_id=?`
		args = append(args, campaignID)
	}
	if cursor != "" {
		if ts, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			where += ` AND created_at > ?`
			args = append(args, ts)
		}
	}
	args = append(args, limit+1)
	var rows []integrationRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+integrationCols+` FROM integrations`+where+` ORDER BY created_at LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list integrations").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.Integration, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, next, nil
}

// GetIntegration loads one integration (merchant-scoped).
func (db *DB) GetIntegration(ctx context.Context, tenantID, merchantID, id string) (*types.Integration, error) {
	var row integrationRow
	err := db.getContext(ctx, &row,
		`SELECT `+integrationCols+` FROM integrations WHERE id=? AND tenant_id=? AND merchant_id=?`,
		id, tenantID, merchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "integration not found").WithMeta("integration_id", id)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load integration").Wrap(err)
	}
	i := row.toDomain()
	return &i, nil
}

// ListIntegrationsForEvent returns active integrations in the tenant/merchant
// that subscribe to eventType, including both campaign-specific (matching
// campaignID) and scope-wide (” campaign) rows. campaignID may be "" (only
// scope-wide rows match). The event-subscription filter is applied in Go so the
// query stays dialect-portable (no JSON containment operators).
func (db *DB) ListIntegrationsForEvent(ctx context.Context, scope types.Scope, campaignID, eventType string) ([]types.Integration, error) {
	where := ` WHERE tenant_id=? AND merchant_id=? AND status=? AND (campaign_id=? OR campaign_id='')`
	args := []any{scope.TenantID, scope.MerchantID, types.IntegrationActive, campaignID}
	var rows []integrationRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+integrationCols+` FROM integrations`+where+` ORDER BY created_at`, args...); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "list integrations for event").Wrap(err)
	}
	out := make([]types.Integration, 0, len(rows))
	for _, r := range rows {
		i := r.toDomain()
		if i.Subscribes(eventType) {
			out = append(out, i)
		}
	}
	return out, nil
}
