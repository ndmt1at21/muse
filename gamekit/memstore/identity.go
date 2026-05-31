package memstore

import (
	"context"
	"sort"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

// --- TenantStore / MerchantStore / IdentityStore / PlayerStore (in-memory) ---

func contactKey(t types.ContactType, value string) string { return string(t) + ":" + value }

// --- TenantStore ---

func (s *Store) CreateTenant(ctx context.Context, t *types.Tenant) (*types.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *t
	s.tenants[t.ID] = &cp
	out := cp
	return &out, nil
}

func (s *Store) GetTenant(ctx context.Context, tenantID string) (*types.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tenants[tenantID]
	if !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "tenant not found").WithMeta("tenant_id", tenantID)
	}
	cp := *t
	return &cp, nil
}

func (s *Store) UpdateTenant(ctx context.Context, t *types.Tenant) (*types.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[t.ID]; !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "tenant not found").WithMeta("tenant_id", t.ID)
	}
	cp := *t
	s.tenants[t.ID] = &cp
	out := cp
	return &out, nil
}

func (s *Store) ListTenants(ctx context.Context, limit int, cursor string) ([]types.Tenant, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]types.Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, "", nil
}

// --- MerchantStore ---

func (s *Store) CreateMerchant(ctx context.Context, m *types.Merchant) (*types.Merchant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *m
	s.merchants[m.TenantID+"|"+m.ID] = &cp
	out := cp
	return &out, nil
}

func (s *Store) GetMerchant(ctx context.Context, tenantID, merchantID string) (*types.Merchant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.merchants[tenantID+"|"+merchantID]
	if !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "merchant not found").WithMeta("merchant_id", merchantID)
	}
	cp := *m
	return &cp, nil
}

func (s *Store) UpdateMerchant(ctx context.Context, m *types.Merchant) (*types.Merchant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := m.TenantID + "|" + m.ID
	if _, ok := s.merchants[k]; !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "merchant not found").WithMeta("merchant_id", m.ID)
	}
	cp := *m
	s.merchants[k] = &cp
	out := cp
	return &out, nil
}

func (s *Store) ListMerchants(ctx context.Context, tenantID string, limit int, cursor string) ([]types.Merchant, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]types.Merchant, 0)
	for _, m := range s.merchants {
		if m.TenantID == tenantID {
			out = append(out, *m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, "", nil
}

// --- IdentityStore ---

func (s *Store) CreateIdentity(ctx context.Context, idn *types.Identity) (*types.Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Enforce global contact uniqueness.
	for _, c := range idn.Contacts {
		if owner, ok := s.contacts[contactKey(c.Type, c.Value)]; ok && owner != idn.ID {
			return nil, gkerr.New(gkerr.ReasonContactConflict, "contact already linked to another identity")
		}
	}
	cp := *idn
	cp.Contacts = append([]types.Contact(nil), idn.Contacts...)
	s.identities[idn.ID] = &cp
	for _, c := range idn.Contacts {
		s.contacts[contactKey(c.Type, c.Value)] = idn.ID
	}
	out := cp
	return &out, nil
}

func (s *Store) GetIdentity(ctx context.Context, identityID string) (*types.Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idn, ok := s.identities[identityID]
	if !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "identity not found").WithMeta("identity_id", identityID)
	}
	cp := *idn
	cp.Contacts = append([]types.Contact(nil), idn.Contacts...)
	return &cp, nil
}

func (s *Store) FindByContact(ctx context.Context, t types.ContactType, value string) (*types.Identity, error) {
	s.mu.Lock()
	id, ok := s.contacts[contactKey(t, value)]
	s.mu.Unlock()
	if !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "no identity for contact")
	}
	return s.GetIdentity(ctx, id)
}

func (s *Store) AddContact(ctx context.Context, identityID string, c types.Contact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idn, ok := s.identities[identityID]
	if !ok {
		return gkerr.New(gkerr.ReasonNotFound, "identity not found").WithMeta("identity_id", identityID)
	}
	k := contactKey(c.Type, c.Value)
	if owner, exists := s.contacts[k]; exists {
		if owner != identityID {
			return gkerr.New(gkerr.ReasonContactConflict, "contact already linked to another identity")
		}
		return nil // idempotent
	}
	idn.Contacts = append(idn.Contacts, c)
	idn.UpdatedAt = c.CreatedAt
	s.contacts[k] = identityID
	return nil
}

// --- PlayerStore ---

func (s *Store) UpsertPlayer(ctx context.Context, p *types.Player) (*types.Player, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idKey := p.TenantID + "|" + p.IdentityID
	if existingID, ok := s.playersByIdentity[idKey]; ok {
		cp := *s.players[existingID]
		return &cp, false, nil
	}
	cp := *p
	if cp.Profile != nil {
		cp.Profile = cloneMap(p.Profile)
	}
	s.players[p.ID] = &cp
	s.playersByIdentity[idKey] = p.ID
	out := cp
	return &out, true, nil
}

func (s *Store) GetPlayer(ctx context.Context, tenantID, playerID string) (*types.Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.players[playerID]
	if !ok || p.TenantID != tenantID {
		return nil, gkerr.New(gkerr.ReasonNotFound, "player not found").WithMeta("player_id", playerID)
	}
	cp := *p
	cp.Profile = cloneMap(p.Profile)
	return &cp, nil
}

func (s *Store) GetPlayerByIdentity(ctx context.Context, tenantID, identityID string) (*types.Player, error) {
	s.mu.Lock()
	id, ok := s.playersByIdentity[tenantID+"|"+identityID]
	s.mu.Unlock()
	if !ok {
		return nil, gkerr.New(gkerr.ReasonNotFound, "player not found")
	}
	return s.GetPlayer(ctx, tenantID, id)
}

func (s *Store) UpdatePlayerProfile(ctx context.Context, tenantID, playerID string, profile map[string]any) (*types.Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.players[playerID]
	if !ok || p.TenantID != tenantID {
		return nil, gkerr.New(gkerr.ReasonNotFound, "player not found").WithMeta("player_id", playerID)
	}
	p.Profile = cloneMap(profile)
	cp := *p
	cp.Profile = cloneMap(p.Profile)
	return &cp, nil
}

func (s *Store) GetTurnBalance(ctx context.Context, tenantID, playerID, scopeKey string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns[turnKey(tenantID, playerID, scopeKey)], nil
}

func (s *Store) GrantTurns(ctx context.Context, tenantID, playerID, scopeKey string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := turnKey(tenantID, playerID, scopeKey)
	v := s.turns[k] + delta
	if v < 0 {
		v = 0
	}
	s.turns[k] = v
	return v, nil
}

func (s *Store) ConsumeTurn(ctx context.Context, tenantID, playerID, scopeKey string) (bool, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := turnKey(tenantID, playerID, scopeKey)
	if s.turns[k] <= 0 {
		return false, 0, nil
	}
	s.turns[k]--
	return true, s.turns[k], nil
}

func turnKey(tenantID, playerID, scopeKey string) string {
	return tenantID + "|" + playerID + "|" + scopeKey
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
