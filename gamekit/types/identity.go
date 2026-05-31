package types

import (
	"strings"
	"time"
)

// --- Tenancy hierarchy: Tenant -> Merchant -> (Campaign -> Game) ---

// Tenant is a platform account / organization — the top of the hierarchy and
// the hard isolation boundary. Every persisted row carries its tenant_id.
type Tenant struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Plan      string         `json:"plan"`
	Settings  TenantSettings `json:"settings"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// TenantSettings are platform-level toggles. WalletScope is the default turn/
// wallet keying for the tenant (campaign|merchant|tenant); a campaign may
// override it. IdentityLinking enables cross-contact identity merge.
type TenantSettings struct {
	IdentityLinking bool   `json:"identity_linking"`
	DefaultLocale   string `json:"default_locale,omitempty"`
	WalletScope     string `json:"wallet_scope,omitempty"` // campaign | merchant | tenant
}

// Merchant is a brand/store under a tenant (one tenant has N merchants).
type Merchant struct {
	ID        string           `json:"id"`
	TenantID  string           `json:"tenant_id"`
	Name      string           `json:"name"`
	Logo      string           `json:"logo,omitempty"`
	Settings  MerchantSettings `json:"settings"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// MerchantSettings are per-merchant overrides. WalletScopeOverride, when set,
// overrides the tenant's wallet_scope for this merchant's campaigns.
type MerchantSettings struct {
	WalletScopeOverride string `json:"wallet_scope_override,omitempty"`
}

// Wallet/turn scope values (also a tenant/campaign setting). Default: campaign.
const (
	WalletScopeCampaign = "campaign"
	WalletScopeMerchant = "merchant"
	WalletScopeTenant   = "tenant"
)

// --- Global identity (internal infra; platform-level dedup) ---

// ContactType is the kind of a verified contact.
type ContactType string

const (
	ContactPhone ContactType = "phone"
	ContactEmail ContactType = "email"
)

// Contact is a verified means of identifying a real person. Each (type, value)
// is GLOBALLY unique and resolves to exactly one identity. Value is normalized:
// phone to E.164-ish (digits + leading +), email lowercased/trimmed.
type Contact struct {
	Type      ContactType `json:"type"`
	Value     string      `json:"value"`
	Verified  bool        `json:"verified"`
	CreatedAt time.Time   `json:"created_at,omitempty"`
}

// Identity is a real person, identified by one or more verified Contacts. It is
// INTERNAL infrastructure (dedup/anti-fraud) and never crosses a tenant
// boundary on its own — tenant data hangs off the tenant-scoped Player.
type Identity struct {
	ID        string    `json:"id"`
	Contacts  []Contact `json:"contacts"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HasContact reports whether the identity already holds a (type, value) contact.
func (i *Identity) HasContact(t ContactType, value string) bool {
	for _, c := range i.Contacts {
		if c.Type == t && c.Value == value {
			return true
		}
	}
	return false
}

// --- Tenant-scoped player (membership) ---

// Player is an Identity participating in one tenant: UNIQUE(tenant_id,
// identity_id). The tenant-scoped profile, collected fields, turn balances,
// wallet, and history all hang off the player — so the same phone yields one
// identity but many isolated players across tenants.
type Player struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	MerchantID string         `json:"merchant_id,omitempty"`
	IdentityID string         `json:"identity_id"`
	Profile    map[string]any `json:"profile,omitempty"` // tenant-scoped collected fields
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// TurnBalance is a player's remaining plays for a scope key (campaign by
// default). Granted by quests/admin, consumed at Play. Phase 6 reads these in
// eligibility; Phase 4 provides the storage + grant/consume primitives.
type TurnBalance struct {
	TenantID  string    `json:"tenant_id"`
	PlayerID  string    `json:"player_id"`
	ScopeKey  string    `json:"scope_key"` // campaign_id | merchant_id | tenant_id
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updated_at"`
}

// --- contact normalization (deterministic; used everywhere a contact enters) ---

// NormalizeContact canonicalizes a contact value for its type so the global
// uniqueness check is stable. Returns the normalized value and whether it is
// well-formed. Phone: strip spaces/dashes/parens, keep an optional leading '+'
// and digits. Email: trim + lowercase, require a single '@' with non-empty
// local and domain parts.
func NormalizeContact(t ContactType, value string) (string, bool) {
	switch t {
	case ContactPhone:
		return normalizePhone(value)
	case ContactEmail:
		return normalizeEmail(value)
	default:
		return "", false
	}
}

func normalizePhone(value string) (string, bool) {
	var b strings.Builder
	for i, r := range strings.TrimSpace(value) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			// drop separators
		default:
			return "", false // letters or other junk → invalid
		}
	}
	out := b.String()
	digits := strings.TrimPrefix(out, "+")
	if len(digits) < 7 || len(digits) > 15 { // E.164 bounds
		return "", false
	}
	return out, true
}

func normalizeEmail(value string) (string, bool) {
	out := strings.ToLower(strings.TrimSpace(value))
	at := strings.IndexByte(out, '@')
	if at <= 0 || at != strings.LastIndexByte(out, '@') || at == len(out)-1 {
		return "", false
	}
	if strings.IndexByte(out[at+1:], '.') < 0 {
		return "", false // domain needs a dot
	}
	return out, true
}
