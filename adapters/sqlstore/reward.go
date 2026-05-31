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
	"github.com/muse/pkg/dialect"
)

// codeIDs mints ids for imported code rows.
var codeIDs defaults.IDGen

const rewardCols = `id, tenant_id, merchant_id, game_id, player_id, prize_id, play_id, name, type, value, code, status, metadata, created_at, claimed_at, fulfilled_at, revoked_at`

type rewardRow struct {
	ID          string       `db:"id"`
	TenantID    string       `db:"tenant_id"`
	MerchantID  string       `db:"merchant_id"`
	GameID      string       `db:"game_id"`
	PlayerID    string       `db:"player_id"`
	PrizeID     string       `db:"prize_id"`
	PlayID      string       `db:"play_id"`
	Name        string       `db:"name"`
	Type        string       `db:"type"`
	Value       int64        `db:"value"`
	Code        string       `db:"code"`
	Status      string       `db:"status"`
	Metadata    []byte       `db:"metadata"`
	CreatedAt   time.Time    `db:"created_at"`
	ClaimedAt   sql.NullTime `db:"claimed_at"`
	FulfilledAt sql.NullTime `db:"fulfilled_at"`
	RevokedAt   sql.NullTime `db:"revoked_at"`
}

func (r rewardRow) toDomain() types.RewardRecord {
	rec := types.RewardRecord{
		ID:        r.ID,
		Scope:     types.Scope{TenantID: r.TenantID, MerchantID: r.MerchantID},
		GameID:    r.GameID,
		PlayerID:  r.PlayerID,
		PrizeID:   r.PrizeID,
		PlayID:    r.PlayID,
		Name:      r.Name,
		Type:      r.Type,
		Value:     r.Value,
		Code:      r.Code,
		Status:    types.RewardStatus(r.Status),
		Metadata:  json.RawMessage(r.Metadata),
		CreatedAt: r.CreatedAt,
	}
	if r.ClaimedAt.Valid {
		rec.ClaimedAt = &r.ClaimedAt.Time
	}
	if r.FulfilledAt.Valid {
		rec.FulfilledAt = &r.FulfilledAt.Time
	}
	if r.RevokedAt.Valid {
		rec.RevokedAt = &r.RevokedAt.Time
	}
	return rec
}

// InsertReward implements ports.RewardStore (runs inside the Play txn).
func (db *DB) InsertReward(ctx context.Context, r *types.RewardRecord) error {
	_, err := db.execContext(ctx,
		`INSERT INTO rewards (`+rewardCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Scope.TenantID, r.Scope.MerchantID, r.GameID, r.PlayerID, r.PrizeID, r.PlayID,
		r.Name, r.Type, r.Value, r.Code, string(r.Status), string(nonEmptyJSON(r.Metadata)),
		r.CreatedAt, nullTime(r.ClaimedAt), nullTime(r.FulfilledAt), nullTime(r.RevokedAt))
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "insert reward").Wrap(err)
	}
	return nil
}

// CountAwards implements ports.RewardStore (non-revoked awards of a prize).
func (db *DB) CountAwards(ctx context.Context, scope types.Scope, prizeID, playerID string, since time.Time) (int, int, error) {
	var c struct {
		Total int `db:"total"`
		Today int `db:"today"`
	}
	err := db.getContext(ctx, &c,
		`SELECT COUNT(*) AS total,
		        COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0) AS today
		   FROM rewards
		  WHERE tenant_id=? AND merchant_id=? AND prize_id=? AND player_id=? AND status <> 'revoked'`,
		since, scope.TenantID, scope.MerchantID, prizeID, playerID)
	if err != nil {
		return 0, 0, gkerr.New(gkerr.ReasonInternal, "count awards").Wrap(err)
	}
	return c.Total, c.Today, nil
}

// PopCode implements ports.RewardStore: atomically claims one available code
// from the prize's pool using SELECT ... FOR UPDATE SKIP LOCKED (supported by
// both Postgres and MySQL 8). Must run inside a transaction (the Play txn).
func (db *DB) PopCode(ctx context.Context, scope types.Scope, prizeID string) (string, bool, error) {
	var row struct {
		ID   string `db:"id"`
		Code string `db:"code"`
	}
	err := db.getContext(ctx, &row,
		`SELECT id, code FROM prize_codes
		  WHERE tenant_id=? AND merchant_id=? AND prize_id=? AND status='available'
		  ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`,
		scope.TenantID, scope.MerchantID, prizeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, gkerr.New(gkerr.ReasonInternal, "select code").Wrap(err)
	}
	_, err = db.execContext(ctx,
		`UPDATE prize_codes SET status='assigned', assigned_at=? WHERE id=? AND status='available'`,
		time.Now().UTC(), row.ID)
	if err != nil {
		return "", false, gkerr.New(gkerr.ReasonInternal, "assign code").Wrap(err)
	}
	return row.Code, true, nil
}

// PopVoucherCode is the atomic, self-contained code pop used by the fulfillment
// dispatcher's voucher_code provider (which runs OUTSIDE the Play txn): it wraps
// PopCode in its own transaction so the SELECT ... FOR UPDATE row lock is held
// through the assigning UPDATE. Satisfies fulfillment.CodePool.
func (db *DB) PopVoucherCode(ctx context.Context, scope types.Scope, prizeID string) (string, bool, error) {
	var (
		code string
		ok   bool
	)
	err := db.WithTx(ctx, func(ctx context.Context) error {
		c, found, e := db.PopCode(ctx, scope, prizeID)
		code, ok = c, found
		return e
	})
	if err != nil {
		return "", false, err
	}
	return code, ok, nil
}

// --- reward lifecycle (Core-level, not a gamekit port) ---

// GetReward fetches a reward by id within scope.
func (db *DB) GetReward(ctx context.Context, scope types.Scope, rewardID string) (*types.RewardRecord, error) {
	var row rewardRow
	err := db.getContext(ctx, &row,
		`SELECT `+rewardCols+` FROM rewards WHERE id=? AND tenant_id=? AND merchant_id=?`,
		rewardID, scope.TenantID, scope.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "reward not found").WithMeta("reward_id", rewardID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load reward").Wrap(err)
	}
	rec := row.toDomain()
	return &rec, nil
}

// ListRewards returns a player's rewards (newest first, cursor by created_at).
func (db *DB) ListRewards(ctx context.Context, scope types.Scope, playerID string, limit int, cursor string) ([]types.RewardRecord, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args := []any{scope.TenantID, scope.MerchantID, playerID}
	where := `tenant_id=? AND merchant_id=? AND player_id=?`
	if cursor != "" {
		if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			where += ` AND created_at < ?`
			args = append(args, t)
		}
	}
	args = append(args, limit+1)
	var rows []rewardRow
	if err := db.selectContext(ctx, &rows,
		`SELECT `+rewardCols+` FROM rewards WHERE `+where+` ORDER BY created_at DESC LIMIT ?`, args...); err != nil {
		return nil, "", gkerr.New(gkerr.ReasonInternal, "list rewards").Wrap(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].CreatedAt.Format(time.RFC3339Nano)
		rows = rows[:limit]
	}
	out := make([]types.RewardRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, next, nil
}

// transition performs a guarded reward status change: it updates only when the
// current status is in `from`, stamping the given timestamp column. A 0-row
// result is disambiguated into NOT_FOUND vs an already-terminal state.
func (db *DB) transition(ctx context.Context, scope types.Scope, rewardID string, from []types.RewardStatus, to types.RewardStatus, stampCol string) (*types.RewardRecord, error) {
	inClause, args := inList(from)
	q := `UPDATE rewards SET status=?, ` + stampCol + `=? WHERE id=? AND tenant_id=? AND merchant_id=? AND status IN (` + inClause + `)`
	execArgs := append([]any{string(to), time.Now().UTC(), rewardID, scope.TenantID, scope.MerchantID}, args...)
	res, err := db.execContext(ctx, q, execArgs...)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "transition reward").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Distinguish missing from wrong-state for a useful error.
		if _, gErr := db.GetReward(ctx, scope, rewardID); gErr != nil {
			return nil, gErr
		}
		return nil, gkerr.Newf(gkerr.ReasonRewardBadState, "reward cannot transition to %s", to).WithMeta("reward_id", rewardID)
	}
	return db.GetReward(ctx, scope, rewardID)
}

// ClaimReward: won → claimed. For prizes on an async-delivery channel this also
// enqueues the outbox task (the on_claim path), atomically with the transition,
// so claiming kicks off delivery without a lost-update window.
func (db *DB) ClaimReward(ctx context.Context, scope types.Scope, rewardID string) (*types.RewardRecord, error) {
	var rec *types.RewardRecord
	err := db.WithTx(ctx, func(ctx context.Context) error {
		r, err := db.transition(ctx, scope, rewardID, []types.RewardStatus{types.RewardWon}, types.RewardClaimed, "claimed_at")
		if err != nil {
			return err
		}
		rec = r
		return db.enqueueClaimDelivery(ctx, scope, r)
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// enqueueClaimDelivery writes an outbox delivery task when a just-claimed
// reward's prize uses an async channel. voucher_code/none are delivered in-app
// (the code is already on the reward), so they need no task. Campaign id is
// best-effort (read from the game) to support admin filtering.
func (db *DB) enqueueClaimDelivery(ctx context.Context, scope types.Scope, r *types.RewardRecord) error {
	prize, err := db.GetPrize(ctx, scope, r.PrizeID)
	if err != nil {
		return err
	}
	if !prize.Fulfillment.NeedsAsyncDelivery() {
		return nil
	}
	campaignID := ""
	if g, gErr := db.GetGame(ctx, scope, r.GameID); gErr == nil {
		campaignID = g.CampaignID
	}
	now := time.Now().UTC()
	return db.EnqueueTask(ctx, &types.FulfillmentTask{
		Scope:         scope,
		RewardID:      r.ID,
		PrizeID:       prize.ID,
		PlayerID:      r.PlayerID,
		GameID:        r.GameID,
		CampaignID:    campaignID,
		Channel:       prize.Fulfillment.ResolvedChannel(),
		ChannelConfig: prize.Fulfillment.ChannelConfig,
		Status:        types.TaskPending,
		MaxAttempts:   prize.Fulfillment.Retry.MaxAttempts,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
}

// FulfillReward: won|claimed → fulfilled.
func (db *DB) FulfillReward(ctx context.Context, scope types.Scope, rewardID string) (*types.RewardRecord, error) {
	return db.transition(ctx, scope, rewardID, []types.RewardStatus{types.RewardWon, types.RewardClaimed}, types.RewardFulfilled, "fulfilled_at")
}

// RevokeReward: won|claimed|fulfilled → revoked.
func (db *DB) RevokeReward(ctx context.Context, scope types.Scope, rewardID string) (*types.RewardRecord, error) {
	return db.transition(ctx, scope, rewardID, []types.RewardStatus{types.RewardWon, types.RewardClaimed, types.RewardFulfilled}, types.RewardRevoked, "revoked_at")
}

// --- code import & stock summary (admin) ---

// ImportCodes bulk-inserts voucher codes into a prize's pool, ignoring
// duplicates. Returns how many were newly imported.
func (db *DB) ImportCodes(ctx context.Context, scope types.Scope, prizeID string, codes []string) (int, error) {
	ignore := "" // engine-specific "insert, skip duplicates"
	suffix := ""
	if db.kind == dialect.Postgres {
		suffix = " ON CONFLICT DO NOTHING"
	} else {
		ignore = "IGNORE "
	}
	imported := 0
	now := time.Now().UTC()
	for _, code := range codes {
		if code == "" {
			continue
		}
		res, err := db.execContext(ctx,
			`INSERT `+ignore+`INTO prize_codes (id, tenant_id, merchant_id, prize_id, code, status, created_at)
			 VALUES (?,?,?,?,?,'available',?)`+suffix,
			idgenCode(), scope.TenantID, scope.MerchantID, prizeID, code, now)
		if err != nil {
			return imported, gkerr.New(gkerr.ReasonInternal, "import code").Wrap(err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			imported++
		}
	}
	return imported, nil
}

// PrizeStockSummary is one row of the admin stock summary.
type PrizeStockSummary struct {
	PrizeID        string `db:"prize_id" json:"prize_id"`
	Name           string `db:"name" json:"name"`
	Total          int64  `db:"total" json:"total"`
	Remaining      int64  `db:"remaining" json:"remaining"`
	Awarded        int64  `db:"awarded" json:"awarded"`
	CodesAvailable int64  `db:"codes_available" json:"codes_available"`
}

// PrizeSummary returns stock + code-pool figures per prize for a game (or all
// prizes in scope when gameID is empty).
func (db *DB) PrizeSummary(ctx context.Context, scope types.Scope, gameID string) ([]PrizeStockSummary, error) {
	args := []any{scope.TenantID, scope.MerchantID}
	where := `p.tenant_id=? AND p.merchant_id=?`
	if gameID != "" {
		where += ` AND p.game_id=?`
		args = append(args, gameID)
	}
	var rows []PrizeStockSummary
	err := db.selectContext(ctx, &rows,
		`SELECT p.id AS prize_id, p.name AS name, p.total AS total, p.remaining AS remaining,
		        (p.total - p.remaining) AS awarded,
		        (SELECT COUNT(*) FROM prize_codes c
		           WHERE c.tenant_id=p.tenant_id AND c.prize_id=p.id AND c.status='available') AS codes_available
		   FROM prizes p WHERE `+where+` ORDER BY p.created_at`, args...)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "prize summary").Wrap(err)
	}
	return rows, nil
}

// --- helpers ---

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func inList[T ~string](vals []T) (string, []any) {
	ph := ""
	args := make([]any, 0, len(vals))
	for i, v := range vals {
		if i > 0 {
			ph += ","
		}
		ph += "?"
		args = append(args, string(v))
	}
	return ph, args
}

func idgenCode() string { return codeIDs.NewID("code") }
