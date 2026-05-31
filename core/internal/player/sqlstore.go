package player

import (
	"context"

	"github.com/muse/adapters/sqlstore"
	"github.com/muse/gamekit/types"
)

// sqlChallengeStore bridges the player.ChallengeStore interface to the sqlstore
// adapter. It lives here (not in sqlstore) because the reusable adapter must not
// depend on Core internals — and Go's internal rule forbids it anyway.
type sqlChallengeStore struct{ db *sqlstore.DB }

// NewSQLChallengeStore adapts a sqlstore.DB to the ChallengeStore interface.
func NewSQLChallengeStore(db *sqlstore.DB) ChallengeStore { return sqlChallengeStore{db: db} }

func (s sqlChallengeStore) CreateChallenge(ctx context.Context, c *Challenge) error {
	return s.db.CreateChallenge(ctx, sqlstore.Challenge{
		ID: c.ID, TenantID: c.Scope.TenantID, MerchantID: c.Scope.MerchantID,
		ContactType: c.ContactType, ContactValue: c.ContactValue, Method: c.Method,
		Secret: c.Secret, CampaignID: c.CampaignID, Consumed: c.Consumed,
		Attempts: c.Attempts, ExpiresAt: c.ExpiresAt, CreatedAt: c.CreatedAt,
	})
}

func (s sqlChallengeStore) GetChallenge(ctx context.Context, tenantID, challengeID string) (*Challenge, error) {
	r, err := s.db.GetChallenge(ctx, tenantID, challengeID)
	if err != nil {
		return nil, err
	}
	return &Challenge{
		ID: r.ID, Scope: types.Scope{TenantID: r.TenantID, MerchantID: r.MerchantID},
		ContactType: r.ContactType, ContactValue: r.ContactValue, Method: r.Method,
		Secret: r.Secret, CampaignID: r.CampaignID, Consumed: r.Consumed,
		Attempts: r.Attempts, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt,
	}, nil
}

func (s sqlChallengeStore) ConsumeChallenge(ctx context.Context, tenantID, challengeID string) (bool, error) {
	return s.db.ConsumeChallenge(ctx, tenantID, challengeID)
}
