package grpcsvc

import (
	"context"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// --- TenantService (platform-admin) ---

func (s *Server) CreateTenant(ctx context.Context, req *gamev1.CreateTenantRequest) (*gamev1.CreateTenantResponse, error) {
	t, err := s.store.CreateTenant(ctx, tenantFromProto(req.GetTenant()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CreateTenantResponse{Tenant: tenantToProto(t)}, nil
}

func (s *Server) GetTenant(ctx context.Context, req *gamev1.GetTenantRequest) (*gamev1.GetTenantResponse, error) {
	t, err := s.store.GetTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetTenantResponse{Tenant: tenantToProto(t)}, nil
}

func (s *Server) UpdateTenant(ctx context.Context, req *gamev1.UpdateTenantRequest) (*gamev1.UpdateTenantResponse, error) {
	t, err := s.store.UpdateTenant(ctx, tenantFromProto(req.GetTenant()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.UpdateTenantResponse{Tenant: tenantToProto(t)}, nil
}

func (s *Server) ListTenants(ctx context.Context, req *gamev1.ListTenantsRequest) (*gamev1.ListTenantsResponse, error) {
	ts, next, err := s.store.ListTenants(ctx, int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.Tenant, 0, len(ts))
	for i := range ts {
		out = append(out, tenantToProto(&ts[i]))
	}
	return &gamev1.ListTenantsResponse{Tenants: out, NextCursor: next}, nil
}

// --- MerchantService (tenant-admin; tenant_id from the request/JWT scope) ---

func (s *Server) CreateMerchant(ctx context.Context, req *gamev1.CreateMerchantRequest) (*gamev1.CreateMerchantResponse, error) {
	tenantID := req.GetTenantId()
	if tenantID == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id is required"))
	}
	m, err := s.store.CreateMerchant(ctx, merchantFromProto(tenantID, req.GetMerchant()))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.CreateMerchantResponse{Merchant: merchantToProto(m)}, nil
}

func (s *Server) GetMerchant(ctx context.Context, req *gamev1.GetMerchantRequest) (*gamev1.GetMerchantResponse, error) {
	m, err := s.store.GetMerchant(ctx, req.GetTenantId(), req.GetMerchantId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetMerchantResponse{Merchant: merchantToProto(m)}, nil
}

func (s *Server) UpdateMerchant(ctx context.Context, req *gamev1.UpdateMerchantRequest) (*gamev1.UpdateMerchantResponse, error) {
	m := req.GetMerchant()
	if m.GetTenantId() == "" {
		return nil, s.fail(gkerr.New(gkerr.ReasonValidationFailed, "tenant_id is required"))
	}
	saved, err := s.store.UpdateMerchant(ctx, merchantFromProto(m.GetTenantId(), m))
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.UpdateMerchantResponse{Merchant: merchantToProto(saved)}, nil
}

func (s *Server) ListMerchants(ctx context.Context, req *gamev1.ListMerchantsRequest) (*gamev1.ListMerchantsResponse, error) {
	ms, next, err := s.store.ListMerchants(ctx, req.GetTenantId(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.Merchant, 0, len(ms))
	for i := range ms {
		out = append(out, merchantToProto(&ms[i]))
	}
	return &gamev1.ListMerchantsResponse{Merchants: out, NextCursor: next}, nil
}

// --- IdentityService (internal: global person) ---

// ResolveOrCreate takes an already-verified contact and resolves/creates the
// identity alone. The normal player login path uses PlayerService.ResolvePlayer
// (identity + tenant player); this is for attaching a known person directly.
func (s *Server) ResolveOrCreate(ctx context.Context, req *gamev1.ResolveOrCreateRequest) (*gamev1.ResolveOrCreateResponse, error) {
	c := req.GetContact()
	t := types.ContactType(c.GetType())
	norm, ok := types.NormalizeContact(t, c.GetValue())
	if !ok {
		return nil, s.fail(gkerr.Newf(gkerr.ReasonValidationFailed, "invalid %s contact", c.GetType()))
	}
	if existing, err := s.store.FindByContact(ctx, t, norm); err == nil {
		return &gamev1.ResolveOrCreateResponse{Identity: identityToProto(existing), Created: false}, nil
	} else if gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
		return nil, s.fail(err)
	}
	created, err := s.store.CreateIdentity(ctx, &types.Identity{
		Contacts: []types.Contact{{Type: t, Value: norm, Verified: true, CreatedAt: s.clock.Now()}},
	})
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.ResolveOrCreateResponse{Identity: identityToProto(created), Created: true}, nil
}

func (s *Server) LinkContact(ctx context.Context, req *gamev1.LinkContactRequest) (*gamev1.LinkContactResponse, error) {
	c := req.GetContact()
	idn, err := s.resolver.LinkContact(ctx, req.GetIdentityId(), types.Contact{
		Type: types.ContactType(c.GetType()), Value: c.GetValue(),
	})
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.LinkContactResponse{Identity: identityToProto(idn)}, nil
}

func (s *Server) GetIdentity(ctx context.Context, req *gamev1.GetIdentityRequest) (*gamev1.GetIdentityResponse, error) {
	idn, err := s.store.GetIdentity(ctx, req.GetIdentityId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetIdentityResponse{Identity: identityToProto(idn)}, nil
}
