// Package identity is the transport-free resolver for the global-person model:
// resolve-or-create an identity from a verified contact, link a second contact
// (identity merge), and upsert the tenant-scoped player. It depends only on the
// IdentityStore + PlayerStore ports — no DB, transport, or auth concern leaks
// in. The hosting layer (Core) calls this after an auth provider verifies a
// contact; an embedder can call it directly against their own port impls.
package identity

import (
	"context"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
)

// Resolver wires the two ports the identity flow needs. IDs mints identity_ids
// (player_ids are minted via IDs too). Clock stamps created/updated times.
type Resolver struct {
	identities ports.IdentityStore
	players    ports.PlayerStore
	ids        ports.IDGen
	clock      ports.Clock
}

// New builds a Resolver.
func New(identities ports.IdentityStore, players ports.PlayerStore, ids ports.IDGen, clock ports.Clock) *Resolver {
	return &Resolver{identities: identities, players: players, ids: ids, clock: clock}
}

// VerifiedLogin is the input after an auth provider has verified a contact: the
// (normalized) contact and the tenant/merchant the person is logging into.
type VerifiedLogin struct {
	Tenant  types.Scope // TenantID required; MerchantID optional (membership hint)
	Contact types.Contact
}

// LoginResult is the resolved person + tenant membership.
type LoginResult struct {
	Identity      *types.Identity
	Player        *types.Player
	IdentityIsNew bool
	PlayerIsNew   bool
}

// ResolveLogin is the core flow: given a freshly verified contact, resolve (or
// create) the global identity, then upsert the tenant-scoped player. Same
// contact value → same identity_id across tenants, but a DIFFERENT player_id
// per tenant (the isolation requirement). The contact is normalized here so
// callers can pass raw input.
func (r *Resolver) ResolveLogin(ctx context.Context, in VerifiedLogin) (*LoginResult, error) {
	if in.Tenant.TenantID == "" {
		return nil, gkerr.New(gkerr.ReasonValidationFailed, "tenant_id is required")
	}
	norm, ok := types.NormalizeContact(in.Contact.Type, in.Contact.Value)
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonValidationFailed, "invalid %s contact", in.Contact.Type).
			WithMeta("contact_type", string(in.Contact.Type))
	}

	idn, idnNew, err := r.resolveOrCreateIdentity(ctx, in.Contact.Type, norm)
	if err != nil {
		return nil, err
	}

	now := r.clock.Now()
	player, playerNew, err := r.players.UpsertPlayer(ctx, &types.Player{
		ID:         r.ids.NewID("player"),
		TenantID:   in.Tenant.TenantID,
		MerchantID: in.Tenant.MerchantID,
		IdentityID: idn.ID,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		return nil, err
	}
	return &LoginResult{Identity: idn, Player: player, IdentityIsNew: idnNew, PlayerIsNew: playerNew}, nil
}

// resolveOrCreateIdentity finds the identity owning a normalized contact, or
// creates a new one holding it. The contact is stored verified (auth already
// proved it).
func (r *Resolver) resolveOrCreateIdentity(ctx context.Context, t types.ContactType, norm string) (*types.Identity, bool, error) {
	existing, err := r.identities.FindByContact(ctx, t, norm)
	if err == nil {
		return existing, false, nil
	}
	if gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
		return nil, false, err
	}
	now := r.clock.Now()
	idn := &types.Identity{
		ID:        r.ids.NewID("idn"),
		Contacts:  []types.Contact{{Type: t, Value: norm, Verified: true, CreatedAt: now}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	created, err := r.identities.CreateIdentity(ctx, idn)
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}

// LinkContact attaches a second verified contact to an existing identity
// (identity merge by contact). If the contact already maps to a DIFFERENT
// identity, the store returns CONTACT_CONFLICT — true merge of two distinct
// identities is out of scope (it would require re-pointing players); the caller
// surfaces the conflict. Returns the refreshed identity.
func (r *Resolver) LinkContact(ctx context.Context, identityID string, c types.Contact) (*types.Identity, error) {
	norm, ok := types.NormalizeContact(c.Type, c.Value)
	if !ok {
		return nil, gkerr.Newf(gkerr.ReasonValidationFailed, "invalid %s contact", c.Type).
			WithMeta("contact_type", string(c.Type))
	}
	// Fast path: if it already maps to a different identity, conflict.
	if owner, err := r.identities.FindByContact(ctx, c.Type, norm); err == nil {
		if owner.ID != identityID {
			return nil, gkerr.New(gkerr.ReasonContactConflict, "contact already linked to another identity").
				WithMeta("contact_type", string(c.Type))
		}
		return owner, nil // already linked to this identity (idempotent)
	} else if gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
		return nil, err
	}
	if err := r.identities.AddContact(ctx, identityID, types.Contact{
		Type: c.Type, Value: norm, Verified: true, CreatedAt: r.clock.Now(),
	}); err != nil {
		return nil, err
	}
	return r.identities.GetIdentity(ctx, identityID)
}
