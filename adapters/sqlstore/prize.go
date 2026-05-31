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

type prizeRow struct {
	ID               string    `db:"id"`
	TenantID         string    `db:"tenant_id"`
	MerchantID       string    `db:"merchant_id"`
	GameID           string    `db:"game_id"`
	Name             string    `db:"name"`
	Type             string    `db:"type"`
	Image            string    `db:"image"`
	Value            int64     `db:"value"`
	Total            int64     `db:"total"`
	Remaining        int64     `db:"remaining"`
	AwardConstraints []byte    `db:"award_constraints"`
	Fulfillment      []byte    `db:"fulfillment"`
	Metadata         []byte    `db:"metadata"`
	CreatedAt        time.Time `db:"created_at"`
}

func (r prizeRow) toDomain() *types.Prize {
	p := &types.Prize{
		ID:        r.ID,
		Scope:     types.Scope{TenantID: r.TenantID, MerchantID: r.MerchantID},
		Name:      r.Name,
		Type:      r.Type,
		Image:     r.Image,
		Value:     r.Value,
		Total:     r.Total,
		Remaining: r.Remaining,
		Metadata:  json.RawMessage(r.Metadata),
	}
	if len(r.AwardConstraints) > 0 {
		_ = json.Unmarshal(r.AwardConstraints, &p.Constraints)
	}
	if len(r.Fulfillment) > 0 {
		_ = json.Unmarshal(r.Fulfillment, &p.Fulfillment)
	}
	return p
}

const prizeCols = `id, tenant_id, merchant_id, game_id, name, type, image, value, total, remaining, award_constraints, fulfillment, metadata, created_at`

// GetPrize implements ports.PrizeStore.
func (db *DB) GetPrize(ctx context.Context, scope types.Scope, prizeID string) (*types.Prize, error) {
	var row prizeRow
	err := db.getContext(ctx, &row,
		`SELECT `+prizeCols+` FROM prizes WHERE id = ? AND tenant_id = ? AND merchant_id = ?`,
		prizeID, scope.TenantID, scope.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "prize not found").WithMeta("prize_id", prizeID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load prize").Wrap(err)
	}
	return row.toDomain(), nil
}

// ListPrizes implements ports.PrizeStore.
func (db *DB) ListPrizes(ctx context.Context, scope types.Scope, gameID string) ([]types.Prize, error) {
	var rows []prizeRow
	err := db.selectContext(ctx, &rows,
		`SELECT `+prizeCols+` FROM prizes WHERE tenant_id = ? AND merchant_id = ? AND game_id = ? ORDER BY created_at`,
		scope.TenantID, scope.MerchantID, gameID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "list prizes").Wrap(err)
	}
	out := make([]types.Prize, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r.toDomain())
	}
	return out, nil
}

// Deduct is the authoritative atomic stock deduction: a single conditional
// UPDATE that decrements iff remaining > 0. RowsAffected == 1 means we won a
// unit; 0 means out of stock. Correct under concurrency on both engines with
// no application-level lock. Runs inside the engine's Play transaction.
func (db *DB) Deduct(ctx context.Context, scope types.Scope, prizeID string) (bool, error) {
	res, err := db.execContext(ctx,
		`UPDATE prizes SET remaining = remaining - 1
		   WHERE id = ? AND tenant_id = ? AND merchant_id = ? AND remaining > 0`,
		prizeID, scope.TenantID, scope.MerchantID)
	if err != nil {
		return false, gkerr.New(gkerr.ReasonInternal, "deduct stock").Wrap(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, gkerr.New(gkerr.ReasonInternal, "deduct rows").Wrap(err)
	}
	return n == 1, nil
}

// UpdatePrize updates mutable prize fields (not stock counters, to avoid
// corrupting in-flight deductions). Returns the refreshed prize.
func (db *DB) UpdatePrize(ctx context.Context, p *types.Prize) (*types.Prize, error) {
	cstr, _ := json.Marshal(p.Constraints)
	ful, _ := json.Marshal(p.Fulfillment)
	meta := nonEmptyJSON(p.Metadata)
	res, err := db.execContext(ctx,
		`UPDATE prizes SET name=?, type=?, image=?, value=?, award_constraints=?, fulfillment=?, metadata=?
		   WHERE id=? AND tenant_id=? AND merchant_id=?`,
		p.Name, p.Type, p.Image, p.Value, string(cstr), string(ful), string(meta),
		p.ID, p.Scope.TenantID, p.Scope.MerchantID)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "update prize").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, gkerr.New(gkerr.ReasonNotFound, "prize not found").WithMeta("prize_id", p.ID)
	}
	return db.GetPrize(ctx, p.Scope, p.ID)
}

// DeletePrize removes a prize.
func (db *DB) DeletePrize(ctx context.Context, scope types.Scope, prizeID string) error {
	res, err := db.execContext(ctx,
		`DELETE FROM prizes WHERE id=? AND tenant_id=? AND merchant_id=?`,
		prizeID, scope.TenantID, scope.MerchantID)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "delete prize").Wrap(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return gkerr.New(gkerr.ReasonNotFound, "prize not found").WithMeta("prize_id", prizeID)
	}
	return nil
}

// CreatePrize inserts a prize (admin seed path).
func (db *DB) CreatePrize(ctx context.Context, p *types.Prize, gameID string) (*types.Prize, error) {
	if p.Remaining == 0 && p.Total > 0 {
		p.Remaining = p.Total
	}
	meta := nonEmptyJSON(p.Metadata)
	cstr, _ := json.Marshal(p.Constraints)
	ful, _ := json.Marshal(p.Fulfillment)
	_, err := db.execContext(ctx,
		`INSERT INTO prizes (`+prizeCols+`)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Scope.TenantID, p.Scope.MerchantID, gameID, p.Name, p.Type, p.Image,
		p.Value, p.Total, p.Remaining, string(cstr), string(ful), string(meta), time.Now().UTC())
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "insert prize").Wrap(err)
	}
	return p, nil
}
