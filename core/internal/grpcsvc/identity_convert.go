package grpcsvc

import (
	"encoding/json"

	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

func tenantToProto(t *types.Tenant) *gamev1.Tenant {
	settings, _ := json.Marshal(t.Settings)
	return &gamev1.Tenant{
		Id: t.ID, Name: t.Name, Plan: t.Plan, Settings: string(settings),
		CreatedAt: ts(t.CreatedAt), UpdatedAt: ts(t.UpdatedAt),
	}
}

func tenantFromProto(p *gamev1.Tenant) *types.Tenant {
	t := &types.Tenant{ID: p.GetId(), Name: p.GetName(), Plan: p.GetPlan()}
	if s := p.GetSettings(); s != "" {
		_ = json.Unmarshal([]byte(s), &t.Settings)
	}
	return t
}

func merchantToProto(m *types.Merchant) *gamev1.Merchant {
	settings, _ := json.Marshal(m.Settings)
	return &gamev1.Merchant{
		Id: m.ID, TenantId: m.TenantID, Name: m.Name, Logo: m.Logo, Settings: string(settings),
		CreatedAt: ts(m.CreatedAt), UpdatedAt: ts(m.UpdatedAt),
	}
}

func merchantFromProto(tenantID string, p *gamev1.Merchant) *types.Merchant {
	m := &types.Merchant{ID: p.GetId(), TenantID: tenantID, Name: p.GetName(), Logo: p.GetLogo()}
	if s := p.GetSettings(); s != "" {
		_ = json.Unmarshal([]byte(s), &m.Settings)
	}
	return m
}

func identityToProto(idn *types.Identity) *gamev1.Identity {
	contacts := make([]*gamev1.Contact, 0, len(idn.Contacts))
	for _, c := range idn.Contacts {
		contacts = append(contacts, &gamev1.Contact{
			Type: pbContactType(c.Type), Value: c.Value, Verified: c.Verified,
		})
	}
	return &gamev1.Identity{
		Id: idn.ID, Contacts: contacts, CreatedAt: ts(idn.CreatedAt), UpdatedAt: ts(idn.UpdatedAt),
	}
}

func playerToProto(p *types.Player) *gamev1.Player {
	profile := ""
	if len(p.Profile) > 0 {
		if b, err := json.Marshal(p.Profile); err == nil {
			profile = string(b)
		}
	}
	return &gamev1.Player{
		Id: p.ID, TenantId: p.TenantID, MerchantId: p.MerchantID, IdentityId: p.IdentityID,
		Profile: profile, CreatedAt: ts(p.CreatedAt), UpdatedAt: ts(p.UpdatedAt),
	}
}
