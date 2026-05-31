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

// --- LeaderboardStore (Phase 7) ---

const lbCols = `id, tenant_id, merchant_id, campaign_id, name, metric, time_window, prize_tiers, anti_cheat, status, created_at, updated_at`

type lbRow struct {
	ID         string    `db:"id"`
	TenantID   string    `db:"tenant_id"`
	MerchantID string    `db:"merchant_id"`
	CampaignID string    `db:"campaign_id"`
	Name       string    `db:"name"`
	Metric     string    `db:"metric"`
	Window     []byte    `db:"time_window"`
	PrizeTiers []byte    `db:"prize_tiers"`
	AntiCheat  []byte    `db:"anti_cheat"`
	Status     string    `db:"status"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

func (r lbRow) toDomain() types.Leaderboard {
	lb := types.Leaderboard{
		ID: r.ID, TenantID: r.TenantID, MerchantID: r.MerchantID, CampaignID: r.CampaignID,
		Name: r.Name, Metric: r.Metric, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if len(r.Window) > 0 {
		_ = json.Unmarshal(r.Window, &lb.Window)
	}
	if len(r.PrizeTiers) > 0 {
		_ = json.Unmarshal(r.PrizeTiers, &lb.PrizeTiers)
	}
	if len(r.AntiCheat) > 0 {
		_ = json.Unmarshal(r.AntiCheat, &lb.AntiCheat)
	}
	return lb
}

// CreateLeaderboard implements ports.LeaderboardStore.
func (db *DB) CreateLeaderboard(ctx context.Context, lb *types.Leaderboard) (*types.Leaderboard, error) {
	if lb.ID == "" {
		lb.ID = tenantIDs.NewID("lb")
	}
	now := time.Now().UTC()
	lb.CreatedAt, lb.UpdatedAt = now, now
	if lb.Status == "" {
		lb.Status = types.LeaderboardActive
	}
	window, _ := json.Marshal(lb.Window)
	tiers, _ := json.Marshal(lb.PrizeTiers)
	ac, _ := json.Marshal(lb.AntiCheat)
	_, err := db.execContext(ctx,
		`INSERT INTO leaderboards (`+lbCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		lb.ID, lb.TenantID, lb.MerchantID, lb.CampaignID, lb.Name, lb.Metric,
		string(window), jsonArrayBytes(tiers), string(ac), lb.Status, lb.CreatedAt, lb.UpdatedAt)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "insert leaderboard").Wrap(err)
	}
	return lb, nil
}

// GetLeaderboard implements ports.LeaderboardStore (merchant-scoped).
func (db *DB) GetLeaderboard(ctx context.Context, tenantID, merchantID, lbID string) (*types.Leaderboard, error) {
	var row lbRow
	err := db.getContext(ctx, &row,
		`SELECT `+lbCols+` FROM leaderboards WHERE id=? AND tenant_id=? AND merchant_id=?`,
		lbID, tenantID, merchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "leaderboard not found").WithMeta("leaderboard_id", lbID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load leaderboard").Wrap(err)
	}
	lb := row.toDomain()
	return &lb, nil
}

// UpdateLeaderboard implements ports.LeaderboardStore (merchant-scoped).
func (db *DB) UpdateLeaderboard(ctx context.Context, lb *types.Leaderboard) (*types.Leaderboard, error) {
	window, _ := json.Marshal(lb.Window)
	tiers, _ := json.Marshal(lb.PrizeTiers)
	ac, _ := json.Marshal(lb.AntiCheat)
	res, err := db.execContext(ctx,
		`UPDATE leaderboards SET campaign_id=?, name=?, metric=?, time_window=?, prize_tiers=?, anti_cheat=?, status=?, updated_at=? WHERE id=? AND tenant_id=? AND merchant_id=?`,
		lb.CampaignID, lb.Name, lb.Metric, string(window), jsonArrayBytes(tiers), string(ac), lb.Status, time.Now().UTC(),
		lb.ID, lb.TenantID, lb.MerchantID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "update leaderboard").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "leaderboard not found").WithMeta("leaderboard_id", lb.ID)
	}
	return db.GetLeaderboard(ctx, lb.TenantID, lb.MerchantID, lb.ID)
}

// ListLeaderboards implements ports.LeaderboardStore (merchant-scoped, optional campaign filter).
func (db *DB) ListLeaderboards(ctx context.Context, tenantID, merchantID, campaignID string, limit int, cursor string) ([]types.Leaderboard, string, error) {
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
	var rows []lbRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+lbCols+` FROM leaderboards`+where+` ORDER BY created_at LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list leaderboards").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.Leaderboard, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, next, nil
}

// ActiveLeaderboardsForCampaign implements ports.LeaderboardStore.
func (db *DB) ActiveLeaderboardsForCampaign(ctx context.Context, tenantID, merchantID, campaignID string) ([]types.Leaderboard, error) {
	var rows []lbRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+lbCols+` FROM leaderboards WHERE tenant_id=? AND merchant_id=? AND campaign_id=? AND status=?`,
		tenantID, merchantID, campaignID, types.LeaderboardActive); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "active leaderboards").Wrap(err)
	}
	out := make([]types.Leaderboard, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, nil
}

// --- entries ---

const entryCols = `tenant_id, leaderboard_id, window_key, player_id, score, plays, state, updated_at`

type entryRow struct {
	TenantID      string    `db:"tenant_id"`
	LeaderboardID string    `db:"leaderboard_id"`
	WindowKey     string    `db:"window_key"`
	PlayerID      string    `db:"player_id"`
	Score         int64     `db:"score"`
	Plays         int64     `db:"plays"`
	State         string    `db:"state"`
	UpdatedAt     time.Time `db:"updated_at"`
}

func (r entryRow) toDomain() types.LeaderboardEntry {
	return types.LeaderboardEntry{
		TenantID: r.TenantID, LeaderboardID: r.LeaderboardID, WindowKey: r.WindowKey,
		PlayerID: r.PlayerID, Score: r.Score, Plays: r.Plays, State: r.State, UpdatedAt: r.UpdatedAt,
	}
}

// UpsertEntry implements ports.LeaderboardStore: atomic score fold + plays++.
func (db *DB) UpsertEntry(ctx context.Context, e *types.LeaderboardEntry, contribution int64, isMax bool) (*types.LeaderboardEntry, error) {
	now := time.Now().UTC()
	scoreExpr := "leaderboard_entries.score + ?"
	if db.kind != dialect.Postgres {
		scoreExpr = "score + ?"
	}
	if isMax {
		if db.kind == dialect.Postgres {
			scoreExpr = "GREATEST(leaderboard_entries.score, ?)"
		} else {
			scoreExpr = "GREATEST(score, ?)"
		}
	}
	var q string
	if db.kind == dialect.Postgres {
		q = `INSERT INTO leaderboard_entries (` + entryCols + `)
		     VALUES (?,?,?,?,?,1,?,?)
		     ON CONFLICT (leaderboard_id, window_key, player_id)
		     DO UPDATE SET score = ` + scoreExpr + `, plays = leaderboard_entries.plays + 1, updated_at = ?`
	} else {
		q = `INSERT INTO leaderboard_entries (` + entryCols + `)
		     VALUES (?,?,?,?,?,1,?,?)
		     ON DUPLICATE KEY UPDATE score = ` + scoreExpr + `, plays = plays + 1, updated_at = ?`
	}
	if _, err := db.execContext(ctx, q,
		e.TenantID, e.LeaderboardID, e.WindowKey, e.PlayerID, contribution, types.EntryActive, now,
		contribution, now); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "upsert leaderboard entry").Wrap(err)
	}
	return db.getEntry(ctx, e.TenantID, e.LeaderboardID, e.WindowKey, e.PlayerID)
}

func (db *DB) getEntry(ctx context.Context, tenantID, lbID, windowKey, playerID string) (*types.LeaderboardEntry, error) {
	var row entryRow
	err := db.getContext(ctx, &row,
		`SELECT `+entryCols+` FROM leaderboard_entries WHERE tenant_id=? AND leaderboard_id=? AND window_key=? AND player_id=?`,
		tenantID, lbID, windowKey, playerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "leaderboard entry not found").WithMeta("player_id", playerID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load leaderboard entry").Wrap(err)
	}
	en := row.toDomain()
	return &en, nil
}

// SetEntryState implements ports.LeaderboardStore.
func (db *DB) SetEntryState(ctx context.Context, tenantID, lbID, windowKey, playerID, state string) (*types.LeaderboardEntry, error) {
	res, err := db.execContext(ctx,
		`UPDATE leaderboard_entries SET state=?, updated_at=? WHERE tenant_id=? AND leaderboard_id=? AND window_key=? AND player_id=?`,
		state, time.Now().UTC(), tenantID, lbID, windowKey, playerID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "set entry state").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "leaderboard entry not found").WithMeta("player_id", playerID)
	}
	return db.getEntry(ctx, tenantID, lbID, windowKey, playerID)
}

// AdjustScore implements ports.LeaderboardStore (signed delta, clamped at 0).
func (db *DB) AdjustScore(ctx context.Context, tenantID, lbID, windowKey, playerID string, delta int64) (*types.LeaderboardEntry, error) {
	expr := "GREATEST(score + ?, 0)"
	if db.kind == dialect.Postgres {
		expr = "GREATEST(leaderboard_entries.score + ?, 0)"
	}
	res, err := db.execContext(ctx,
		`UPDATE leaderboard_entries SET score=`+expr+`, updated_at=? WHERE tenant_id=? AND leaderboard_id=? AND window_key=? AND player_id=?`,
		delta, time.Now().UTC(), tenantID, lbID, windowKey, playerID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "adjust score").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "leaderboard entry not found").WithMeta("player_id", playerID)
	}
	return db.getEntry(ctx, tenantID, lbID, windowKey, playerID)
}

// rankExpr is the correlated subquery computing a 1-based rank (higher score
// first, ties broken by earlier updated_at). Excludes disqualified entries.
const rankExpr = `(SELECT COUNT(*)+1 FROM leaderboard_entries e2
	WHERE e2.tenant_id=e.tenant_id AND e2.leaderboard_id=e.leaderboard_id AND e2.window_key=e.window_key
	  AND e2.state <> 'disqualified'
	  AND (e2.score > e.score OR (e2.score = e.score AND e2.updated_at < e.updated_at)))`

type rankedRow struct {
	entryRow
	Rank int64 `db:"rnk"`
}

// Rankings implements ports.LeaderboardStore (page of ranked, non-disqualified entries + total).
func (db *DB) Rankings(ctx context.Context, tenantID, lbID, windowKey string, limit, offset int) ([]types.RankedEntry, int64, error) {
	limit = clampLimit(limit)
	if offset < 0 {
		offset = 0
	}
	var rows []rankedRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+entryColsPrefixed("e")+`, `+rankExpr+` AS rnk
		 FROM leaderboard_entries e
		 WHERE e.tenant_id=? AND e.leaderboard_id=? AND e.window_key=? AND e.state <> 'disqualified'
		 ORDER BY e.score DESC, e.updated_at ASC LIMIT ? OFFSET ?`,
		tenantID, lbID, windowKey, limit, offset); err != nil {
		return nil, 0, gkerr.New(gkerr.ReasonInternal, "rankings").Wrap(err)
	}
	var total int64
	if err := db.getContext(ctx, &total,
		`SELECT COUNT(*) FROM leaderboard_entries WHERE tenant_id=? AND leaderboard_id=? AND window_key=? AND state <> 'disqualified'`,
		tenantID, lbID, windowKey); err != nil {
		return nil, 0, gkerr.New(gkerr.ReasonInternal, "rankings count").Wrap(err)
	}
	out := make([]types.RankedEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.RankedEntry{LeaderboardEntry: r.entryRow.toDomain(), Rank: r.Rank})
	}
	return out, total, nil
}

// PlayerRank implements ports.LeaderboardStore.
func (db *DB) PlayerRank(ctx context.Context, tenantID, lbID, windowKey, playerID string) (*types.RankedEntry, error) {
	var row rankedRow
	err := db.getContext(ctx, &row,
		`SELECT `+entryColsPrefixed("e")+`, `+rankExpr+` AS rnk
		 FROM leaderboard_entries e
		 WHERE e.tenant_id=? AND e.leaderboard_id=? AND e.window_key=? AND e.player_id=? AND e.state <> 'disqualified'`,
		tenantID, lbID, windowKey, playerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "no ranking for player").WithMeta("player_id", playerID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "player rank").Wrap(err)
	}
	return &types.RankedEntry{LeaderboardEntry: row.entryRow.toDomain(), Rank: row.Rank}, nil
}

// SnapshotRanking implements ports.LeaderboardStore: all award-eligible
// (state=active) entries ranked, unpaginated, for finalize.
func (db *DB) SnapshotRanking(ctx context.Context, tenantID, lbID, windowKey string) ([]types.RankedEntry, error) {
	var rows []entryRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+entryCols+` FROM leaderboard_entries
		 WHERE tenant_id=? AND leaderboard_id=? AND window_key=? AND state='active'
		 ORDER BY score DESC, updated_at ASC`,
		tenantID, lbID, windowKey); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "snapshot ranking").Wrap(err)
	}
	out := make([]types.RankedEntry, 0, len(rows))
	for i, r := range rows {
		out = append(out, types.RankedEntry{LeaderboardEntry: r.toDomain(), Rank: int64(i + 1)})
	}
	return out, nil
}

// DeleteEntries implements ports.LeaderboardStore (reset a window).
func (db *DB) DeleteEntries(ctx context.Context, tenantID, lbID, windowKey string) (int64, error) {
	res, err := db.execContext(ctx,
		`DELETE FROM leaderboard_entries WHERE tenant_id=? AND leaderboard_id=? AND window_key=?`,
		tenantID, lbID, windowKey)
	if err != nil {
		return 0, gkerr.New(gkerr.ReasonInternal, "delete entries").Wrap(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// entryColsPrefixed qualifies the entry columns for a join/alias select.
func entryColsPrefixed(alias string) string {
	return alias + `.tenant_id, ` + alias + `.leaderboard_id, ` + alias + `.window_key, ` +
		alias + `.player_id, ` + alias + `.score, ` + alias + `.plays, ` + alias + `.state, ` + alias + `.updated_at`
}

// jsonArrayBytes never returns the JSON "null" for a nil slice (NOT NULL columns).
func jsonArrayBytes(b []byte) string {
	if len(b) == 0 || string(b) == "null" {
		return "[]"
	}
	return string(b)
}
