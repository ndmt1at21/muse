package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// --- IdentityStore ---

// CreateIdentity inserts the identity row plus its initial contacts in one txn.
// A duplicate contact (already owned by anyone) surfaces as CONTACT_CONFLICT.
func (db *DB) CreateIdentity(ctx context.Context, idn *types.Identity) (*types.Identity, error) {
	if idn.ID == "" {
		idn.ID = tenantIDs.NewID("idn")
	}
	now := time.Now().UTC()
	if idn.CreatedAt.IsZero() {
		idn.CreatedAt = now
	}
	idn.UpdatedAt = now
	err := db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := db.execContext(ctx,
			`INSERT INTO identities (id, created_at, updated_at) VALUES (?,?,?)`,
			idn.ID, idn.CreatedAt, idn.UpdatedAt); err != nil {
			return gkerr.New(gkerr.ReasonInternal, "insert identity").Wrap(err)
		}
		for _, c := range idn.Contacts {
			if err := db.insertContact(ctx, idn.ID, c); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return idn, nil
}

// GetIdentity loads an identity and its contacts.
func (db *DB) GetIdentity(ctx context.Context, identityID string) (*types.Identity, error) {
	var head struct {
		ID        string    `db:"id"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	err := db.getContext(ctx, &head,
		`SELECT id, created_at, updated_at FROM identities WHERE id=?`, identityID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "identity not found").WithMeta("identity_id", identityID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load identity").Wrap(err)
	}
	contacts, err := db.listContacts(ctx, identityID)
	if err != nil {
		return nil, err
	}
	return &types.Identity{ID: head.ID, Contacts: contacts, CreatedAt: head.CreatedAt, UpdatedAt: head.UpdatedAt}, nil
}

// FindByContact resolves the identity owning a normalized contact.
func (db *DB) FindByContact(ctx context.Context, t types.ContactType, value string) (*types.Identity, error) {
	var idnID string
	err := db.getContext(ctx, &idnID,
		`SELECT identity_id FROM identity_contacts WHERE contact_type=? AND contact_value=?`, string(t), value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "no identity for contact")
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "find by contact").Wrap(err)
	}
	return db.GetIdentity(ctx, idnID)
}

// AddContact links a verified contact, enforcing global uniqueness. Idempotent
// if it already belongs to this identity; CONTACT_CONFLICT if to another.
func (db *DB) AddContact(ctx context.Context, identityID string, c types.Contact) error {
	return db.WithTx(ctx, func(ctx context.Context) error {
		var owner string
		err := db.getContext(ctx, &owner,
			`SELECT identity_id FROM identity_contacts WHERE contact_type=? AND contact_value=?`,
			string(c.Type), c.Value)
		if err == nil {
			if owner != identityID {
				return gkerr.New(gkerr.ReasonContactConflict, "contact already linked to another identity").
					WithMeta("contact_type", string(c.Type))
			}
			return nil // idempotent
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return gkerr.New(gkerr.ReasonInternal, "check contact").Wrap(err)
		}
		if err := db.insertContact(ctx, identityID, c); err != nil {
			return err
		}
		_, _ = db.execContext(ctx, `UPDATE identities SET updated_at=? WHERE id=?`, time.Now().UTC(), identityID)
		return nil
	})
}

// insertContact inserts one contact row, mapping a uniqueness violation to
// CONTACT_CONFLICT. Runs inside the caller's transaction.
func (db *DB) insertContact(ctx context.Context, identityID string, c types.Contact) error {
	createdAt := c.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := db.execContext(ctx,
		`INSERT INTO identity_contacts (identity_id, contact_type, contact_value, verified, created_at)
		 VALUES (?,?,?,?,?)`,
		identityID, string(c.Type), c.Value, c.Verified, createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return gkerr.New(gkerr.ReasonContactConflict, "contact already linked to another identity").
				WithMeta("contact_type", string(c.Type))
		}
		return gkerr.New(gkerr.ReasonInternal, "insert contact").Wrap(err)
	}
	return nil
}

func (db *DB) listContacts(ctx context.Context, identityID string) ([]types.Contact, error) {
	var rows []struct {
		Type      string    `db:"contact_type"`
		Value     string    `db:"contact_value"`
		Verified  bool      `db:"verified"`
		CreatedAt time.Time `db:"created_at"`
	}
	if err := db.selectContext(ctx, &rows,
		`SELECT contact_type, contact_value, verified, created_at FROM identity_contacts
		  WHERE identity_id=? ORDER BY created_at`, identityID); err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "list contacts").Wrap(err)
	}
	out := make([]types.Contact, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.Contact{
			Type: types.ContactType(r.Type), Value: r.Value, Verified: r.Verified, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}
