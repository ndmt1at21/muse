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

// --- CampaignStore (Phase 5; campaign-scoped: tenant_id + merchant_id) ---

const campaignCols = `id, tenant_id, merchant_id, name, status, start_date, end_date, channels, games, quests, settings, created_at, updated_at`

type campaignRow struct {
	ID         string     `db:"id"`
	TenantID   string     `db:"tenant_id"`
	MerchantID string     `db:"merchant_id"`
	Name       string     `db:"name"`
	Status     string     `db:"status"`
	StartDate  *time.Time `db:"start_date"`
	EndDate    *time.Time `db:"end_date"`
	Channels   []byte     `db:"channels"`
	Games      []byte     `db:"games"`
	Quests     []byte     `db:"quests"`
	Settings   []byte     `db:"settings"`
	CreatedAt  time.Time  `db:"created_at"`
	UpdatedAt  time.Time  `db:"updated_at"`
}

func (r campaignRow) toDomain() types.Campaign {
	c := types.Campaign{
		ID: r.ID, TenantID: r.TenantID, MerchantID: r.MerchantID, Name: r.Name,
		Status: types.CampaignStatus(r.Status), StartDate: r.StartDate, EndDate: r.EndDate,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if len(r.Channels) > 0 {
		_ = json.Unmarshal(r.Channels, &c.Channels)
	}
	if len(r.Games) > 0 {
		_ = json.Unmarshal(r.Games, &c.Games)
	}
	if len(r.Quests) > 0 {
		_ = json.Unmarshal(r.Quests, &c.Quests)
	}
	if len(r.Settings) > 0 {
		_ = json.Unmarshal(r.Settings, &c.Settings)
	}
	return c
}

// jsonArray marshals a string slice to a JSON array, never returning "null"
// (NOT NULL JSON columns reject it on MySQL).
func jsonArray(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// CreateCampaign implements ports.CampaignStore.
func (db *DB) CreateCampaign(ctx context.Context, c *types.Campaign) (*types.Campaign, error) {
	if c.ID == "" {
		c.ID = tenantIDs.NewID("campaign")
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	if c.Status == "" {
		c.Status = types.CampaignDraft
	}
	settings, _ := json.Marshal(c.Settings)
	_, err := db.execContext(ctx,
		`INSERT INTO campaigns (`+campaignCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.TenantID, c.MerchantID, c.Name, string(c.Status), c.StartDate, c.EndDate,
		jsonArray(c.Channels), jsonArray(c.Games), jsonArray(c.Quests), string(settings),
		c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "insert campaign").Wrap(err)
	}
	return c, nil
}

// GetCampaign implements ports.CampaignStore (merchant-scoped).
func (db *DB) GetCampaign(ctx context.Context, tenantID, merchantID, campaignID string) (*types.Campaign, error) {
	var row campaignRow
	err := db.getContext(ctx, &row,
		`SELECT `+campaignCols+` FROM campaigns WHERE id=? AND tenant_id=? AND merchant_id=?`,
		campaignID, tenantID, merchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "campaign not found").WithMeta("campaign_id", campaignID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load campaign").Wrap(err)
	}
	c := row.toDomain()
	return &c, nil
}

// GetCampaignByID implements ports.CampaignStore (id alone; for the public widget
// config, where the caller knows only the globally-unique campaign id).
func (db *DB) GetCampaignByID(ctx context.Context, campaignID string) (*types.Campaign, error) {
	var row campaignRow
	err := db.getContext(ctx, &row,
		`SELECT `+campaignCols+` FROM campaigns WHERE id=?`, campaignID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "campaign not found").WithMeta("campaign_id", campaignID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load campaign").Wrap(err)
	}
	c := row.toDomain()
	return &c, nil
}

// UpdateCampaign implements ports.CampaignStore (merchant-scoped).
func (db *DB) UpdateCampaign(ctx context.Context, c *types.Campaign) (*types.Campaign, error) {
	settings, _ := json.Marshal(c.Settings)
	res, err := db.execContext(ctx,
		`UPDATE campaigns SET name=?, status=?, start_date=?, end_date=?, channels=?, games=?, quests=?, settings=?, updated_at=? WHERE id=? AND tenant_id=? AND merchant_id=?`,
		c.Name, string(c.Status), c.StartDate, c.EndDate,
		jsonArray(c.Channels), jsonArray(c.Games), jsonArray(c.Quests), string(settings), time.Now().UTC(),
		c.ID, c.TenantID, c.MerchantID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "update campaign").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "campaign not found").WithMeta("campaign_id", c.ID)
	}
	return db.GetCampaign(ctx, c.TenantID, c.MerchantID, c.ID)
}

// ListCampaigns implements ports.CampaignStore (merchant-scoped, cursor by created_at).
func (db *DB) ListCampaigns(ctx context.Context, tenantID, merchantID string, limit int, cursor string) ([]types.Campaign, string, error) {
	limit = clampLimit(limit)
	where := ` WHERE tenant_id=? AND merchant_id=?`
	args := []any{tenantID, merchantID}
	if cursor != "" {
		if ts, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			where += ` AND created_at > ?`
			args = append(args, ts)
		}
	}
	args = append(args, limit+1)
	var rows []campaignRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+campaignCols+` FROM campaigns`+where+` ORDER BY created_at LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list campaigns").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.Campaign, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, next, nil
}

// DuplicateCampaign clones a campaign (and its game/quest links + settings) under
// the same merchant with a fresh id and a draft status. A hosting-layer helper
// (not a port method), mirroring PrizeSummary.
func (db *DB) DuplicateCampaign(ctx context.Context, tenantID, merchantID, campaignID, newName string) (*types.Campaign, error) {
	src, err := db.GetCampaign(ctx, tenantID, merchantID, campaignID)
	if err != nil {
		return nil, err
	}
	if newName == "" {
		newName = src.Name + " (copy)"
	}
	dup := &types.Campaign{
		TenantID: src.TenantID, MerchantID: src.MerchantID, Name: newName,
		Status:    types.CampaignDraft, // a clone always starts as a draft
		StartDate: src.StartDate, EndDate: src.EndDate,
		Channels: src.Channels, Games: src.Games, Quests: src.Quests, Settings: src.Settings,
	}
	return db.CreateCampaign(ctx, dup)
}

// CampaignAnalytics rolls up plays/wins/unique-players for a campaign's games. A
// hosting-layer query (not a port method). Plays come from play_history, wins
// from non-revoked reward rows, both restricted to games whose campaign_id
// matches — all under the same tenant/merchant.
func (db *DB) CampaignAnalytics(ctx context.Context, tenantID, merchantID, campaignID string) (*types.CampaignAnalytics, error) {
	// Confirm the campaign exists in this scope (so analytics for a foreign or
	// missing campaign is NOT_FOUND, not a silent zero rollup).
	if _, err := db.GetCampaign(ctx, tenantID, merchantID, campaignID); err != nil {
		return nil, err
	}
	out := &types.CampaignAnalytics{CampaignID: campaignID}

	gamesSub := `SELECT id FROM games WHERE tenant_id=? AND campaign_id=?`
	var plays struct {
		Total   int64 `db:"total"`
		Players int64 `db:"players"`
	}
	if err := db.getContext(ctx, &plays,
		`SELECT COUNT(*) AS total, COUNT(DISTINCT player_id) AS players FROM play_history
		 WHERE tenant_id=? AND game_id IN (`+gamesSub+`)`,
		tenantID, tenantID, campaignID); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "campaign play stats").Wrap(err)
	}
	out.TotalPlays, out.UniquePlayers = plays.Total, plays.Players

	var wins int64
	if err := db.getContext(ctx, &wins,
		`SELECT COUNT(*) FROM rewards
		 WHERE tenant_id=? AND status<>'revoked' AND game_id IN (`+gamesSub+`)`,
		tenantID, tenantID, campaignID); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "campaign win stats").Wrap(err)
	}
	out.TotalWins = wins
	if out.TotalPlays > 0 {
		out.Conversion = float64(out.TotalWins) / float64(out.TotalPlays)
	}
	return out, nil
}
