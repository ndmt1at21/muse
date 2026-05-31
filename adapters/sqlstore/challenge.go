package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/muse/gamekit/gkerr"
)

// Challenge is the persisted auth challenge (a plain data holder; the auth
// semantics live in core/internal/player). Kept here so the SQL access stays in
// the adapter without it importing Core internals.
type Challenge struct {
	ID           string    `db:"id"`
	TenantID     string    `db:"tenant_id"`
	MerchantID   string    `db:"merchant_id"`
	ContactType  string    `db:"contact_type"`
	ContactValue string    `db:"contact_value"`
	Method       string    `db:"method"`
	Secret       string    `db:"secret"`
	CampaignID   string    `db:"campaign_id"`
	Consumed     bool      `db:"consumed"`
	Attempts     int       `db:"attempts"`
	ExpiresAt    time.Time `db:"expires_at"`
	CreatedAt    time.Time `db:"created_at"`
}

const challengeCols = `id, tenant_id, merchant_id, contact_type, contact_value, method, secret, campaign_id, consumed, attempts, expires_at, created_at`

// CreateChallenge inserts a pending auth challenge.
func (db *DB) CreateChallenge(ctx context.Context, c Challenge) error {
	_, err := db.execContext(ctx,
		`INSERT INTO auth_challenges (`+challengeCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.TenantID, c.MerchantID, c.ContactType, c.ContactValue, c.Method, c.Secret,
		c.CampaignID, c.Consumed, c.Attempts, c.ExpiresAt, c.CreatedAt)
	if err != nil {
		return gkerr.New(gkerr.ReasonInternal, "insert challenge").Wrap(err)
	}
	return nil
}

// GetChallenge loads a challenge by id within a tenant.
func (db *DB) GetChallenge(ctx context.Context, tenantID, challengeID string) (*Challenge, error) {
	var c Challenge
	err := db.getContext(ctx, &c,
		`SELECT `+challengeCols+` FROM auth_challenges WHERE id=? AND tenant_id=?`, challengeID, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "challenge not found").WithMeta("challenge_id", challengeID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load challenge").Wrap(err)
	}
	return &c, nil
}

// ConsumeChallenge atomically flips consumed false→true; ok=false means it was
// already consumed (single-use guarantee even under a double-submit race).
func (db *DB) ConsumeChallenge(ctx context.Context, tenantID, challengeID string) (bool, error) {
	res, err := db.execContext(ctx,
		`UPDATE auth_challenges SET consumed=? WHERE id=? AND tenant_id=? AND consumed=?`,
		true, challengeID, tenantID, false)
	if err != nil {
		return false, gkerr.New(gkerr.ReasonInternal, "consume challenge").Wrap(err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
