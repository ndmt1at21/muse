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

// --- QuestStore (Phase 6; campaign-scoped: tenant_id + merchant_id) ---

const questCols = `id, tenant_id, merchant_id, campaign_id, type, name, status, reward, config, created_at, updated_at`

type questRow struct {
	ID         string    `db:"id"`
	TenantID   string    `db:"tenant_id"`
	MerchantID string    `db:"merchant_id"`
	CampaignID string    `db:"campaign_id"`
	Type       string    `db:"type"`
	Name       string    `db:"name"`
	Status     string    `db:"status"`
	Reward     []byte    `db:"reward"`
	Config     []byte    `db:"config"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

func (r questRow) toDomain() types.Quest {
	q := types.Quest{
		ID: r.ID, TenantID: r.TenantID, MerchantID: r.MerchantID, CampaignID: r.CampaignID,
		Type: r.Type, Name: r.Name, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if len(r.Reward) > 0 {
		_ = json.Unmarshal(r.Reward, &q.Reward)
	}
	if len(r.Config) > 0 {
		q.Config = json.RawMessage(r.Config)
	}
	return q
}

// CreateQuest implements ports.QuestStore.
func (db *DB) CreateQuest(ctx context.Context, q *types.Quest) (*types.Quest, error) {
	if q.ID == "" {
		q.ID = tenantIDs.NewID("quest")
	}
	now := time.Now().UTC()
	q.CreatedAt, q.UpdatedAt = now, now
	if q.Status == "" {
		q.Status = types.QuestActive
	}
	reward, _ := json.Marshal(q.Reward)
	_, err := db.execContext(ctx,
		`INSERT INTO quests (`+questCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		q.ID, q.TenantID, q.MerchantID, q.CampaignID, q.Type, q.Name, q.Status,
		string(reward), string(nonEmptyJSON(q.Config)), q.CreatedAt, q.UpdatedAt)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "insert quest").Wrap(err)
	}
	return q, nil
}

// GetQuest implements ports.QuestStore (merchant-scoped).
func (db *DB) GetQuest(ctx context.Context, tenantID, merchantID, questID string) (*types.Quest, error) {
	var row questRow
	err := db.getContext(ctx, &row,
		`SELECT `+questCols+` FROM quests WHERE id=? AND tenant_id=? AND merchant_id=?`,
		questID, tenantID, merchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "quest not found").WithMeta("quest_id", questID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load quest").Wrap(err)
	}
	q := row.toDomain()
	return &q, nil
}

// UpdateQuest implements ports.QuestStore (merchant-scoped).
func (db *DB) UpdateQuest(ctx context.Context, q *types.Quest) (*types.Quest, error) {
	reward, _ := json.Marshal(q.Reward)
	res, err := db.execContext(ctx,
		`UPDATE quests SET campaign_id=?, type=?, name=?, status=?, reward=?, config=?, updated_at=? WHERE id=? AND tenant_id=? AND merchant_id=?`,
		q.CampaignID, q.Type, q.Name, q.Status, string(reward), string(nonEmptyJSON(q.Config)), time.Now().UTC(),
		q.ID, q.TenantID, q.MerchantID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "update quest").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "quest not found").WithMeta("quest_id", q.ID)
	}
	return db.GetQuest(ctx, q.TenantID, q.MerchantID, q.ID)
}

// DeleteQuest implements ports.QuestStore (merchant-scoped).
func (db *DB) DeleteQuest(ctx context.Context, tenantID, merchantID, questID string) error {
	res, err := db.execContext(ctx,
		`DELETE FROM quests WHERE id=? AND tenant_id=? AND merchant_id=?`, questID, tenantID, merchantID)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "delete quest").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gkerr.New(gkerr.ReasonNotFound, "quest not found").WithMeta("quest_id", questID)
	}
	return nil
}

// ListQuests implements ports.QuestStore. An empty campaignID lists all quests
// under the merchant; otherwise it filters by campaign.
func (db *DB) ListQuests(ctx context.Context, tenantID, merchantID, campaignID string, limit int, cursor string) ([]types.Quest, string, error) {
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
	var rows []questRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+questCols+` FROM quests`+where+` ORDER BY created_at LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list quests").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.Quest, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, next, nil
}

// RecordCompletion implements ports.QuestStore (inside the grant txn).
func (db *DB) RecordCompletion(ctx context.Context, c *types.QuestCompletion) error {
	if c.ID == "" {
		c.ID = tenantIDs.NewID("qcmp")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	_, err := db.execContext(ctx,
		`INSERT INTO quest_completions (id, tenant_id, quest_id, player_id, created_at) VALUES (?,?,?,?,?)`,
		c.ID, c.TenantID, c.QuestID, c.PlayerID, c.CreatedAt)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "insert quest completion").Wrap(err)
	}
	return nil
}

// CountCompletions implements ports.QuestStore: a player's completions of a
// quest (all time) and since `since` (start of today), plus the last time.
func (db *DB) CountCompletions(ctx context.Context, tenantID, questID, playerID string, since time.Time) (int, int, *time.Time, error) {
	var row struct {
		Total int        `db:"total"`
		Today int        `db:"today"`
		Last  *time.Time `db:"last_at"`
	}
	err := db.getContext(ctx, &row,
		`SELECT COUNT(*) AS total,
		        COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0) AS today,
		        MAX(created_at) AS last_at
		 FROM quest_completions WHERE tenant_id=? AND quest_id=? AND player_id=?`,
		since, tenantID, questID, playerID)
	if err != nil {
		return 0, 0, nil, gkerr.New(gkerr.ReasonInternal, "count quest completions").Wrap(err)
	}
	return row.Total, row.Today, row.Last, nil
}
