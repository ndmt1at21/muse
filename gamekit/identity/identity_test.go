package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/identity"
	"github.com/muse/gamekit/memstore"
	"github.com/muse/gamekit/types"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func newResolver() (*identity.Resolver, *memstore.Store) {
	store := memstore.New()
	return identity.New(store, store, &memstore.SeqIDGen{}, fixedClock{t: time.Now().UTC()}), store
}

func phone(v string) types.Contact { return types.Contact{Type: types.ContactPhone, Value: v} }

// The headline Phase 4 requirement: the same real phone resolves to ONE global
// identity but DIFFERENT, isolated players in different tenants.
func TestSamePhoneOneIdentityIsolatedPlayers(t *testing.T) {
	r, _ := newResolver()
	ctx := context.Background()

	// tenant_a: spaced format.
	a, err := r.ResolveLogin(ctx, identity.VerifiedLogin{
		Tenant: types.Scope{TenantID: "tenant_a"}, Contact: phone("+84 901 234 567"),
	})
	if err != nil {
		t.Fatalf("login a: %v", err)
	}
	// tenant_b: SAME number, dashed format — must normalize to the same identity.
	b, err := r.ResolveLogin(ctx, identity.VerifiedLogin{
		Tenant: types.Scope{TenantID: "tenant_b"}, Contact: phone("+84-901-234-567"),
	})
	if err != nil {
		t.Fatalf("login b: %v", err)
	}

	if a.Identity.ID != b.Identity.ID {
		t.Errorf("same phone must yield one identity: %s vs %s", a.Identity.ID, b.Identity.ID)
	}
	if a.Player.ID == b.Player.ID {
		t.Errorf("players across tenants must be isolated, both = %s", a.Player.ID)
	}
	if !a.IdentityIsNew || b.IdentityIsNew {
		t.Errorf("identity should be new for a, reused for b (a=%v b=%v)", a.IdentityIsNew, b.IdentityIsNew)
	}
	if !a.PlayerIsNew || !b.PlayerIsNew {
		t.Error("both players are first-seen in their tenant, should be new")
	}
}

// Re-login in the same tenant returns the same player (idempotent upsert).
func TestReloginSameTenantSamePlayer(t *testing.T) {
	r, _ := newResolver()
	ctx := context.Background()
	first, _ := r.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: types.Scope{TenantID: "t1"}, Contact: phone("+84905000111")})
	second, err := r.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: types.Scope{TenantID: "t1"}, Contact: phone("+84905000111")})
	if err != nil {
		t.Fatal(err)
	}
	if first.Player.ID != second.Player.ID {
		t.Errorf("re-login should reuse player: %s vs %s", first.Player.ID, second.Player.ID)
	}
	if second.PlayerIsNew {
		t.Error("second login player should not be new")
	}
}

// Email and phone for the same person link into one identity.
func TestLinkContactMergesIntoOneIdentity(t *testing.T) {
	r, store := newResolver()
	ctx := context.Background()
	res, _ := r.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: types.Scope{TenantID: "t1"}, Contact: phone("+84905000222")})

	idn, err := r.LinkContact(ctx, res.Identity.ID, types.Contact{Type: types.ContactEmail, Value: "  Alice@Example.COM "})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if len(idn.Contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(idn.Contacts))
	}
	// The email normalizes to lowercase/trimmed and now resolves to the SAME identity.
	found, err := store.FindByContact(ctx, types.ContactEmail, "alice@example.com")
	if err != nil || found.ID != res.Identity.ID {
		t.Errorf("normalized email should resolve to the same identity, got %v err=%v", found, err)
	}
	// Logging in by the email (any tenant) hits that same identity.
	byEmail, _ := r.ResolveLogin(ctx, identity.VerifiedLogin{
		Tenant: types.Scope{TenantID: "t2"}, Contact: types.Contact{Type: types.ContactEmail, Value: "alice@example.com"},
	})
	if byEmail.Identity.ID != res.Identity.ID {
		t.Error("login by linked email should resolve to the same identity")
	}
}

// Linking a contact already owned by a different identity is a CONTACT_CONFLICT.
func TestLinkContactConflict(t *testing.T) {
	r, _ := newResolver()
	ctx := context.Background()
	a, _ := r.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: types.Scope{TenantID: "t1"}, Contact: phone("+84905000333")})
	b, _ := r.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: types.Scope{TenantID: "t1"}, Contact: phone("+84905000444")})

	_, err := r.LinkContact(ctx, a.Identity.ID, phone("+84905000444")) // b's phone
	if gkerr.ReasonOf(err) != gkerr.ReasonContactConflict {
		t.Errorf("expected CONTACT_CONFLICT, got %v", err)
	}
	_ = b
}

func TestInvalidContactRejected(t *testing.T) {
	r, _ := newResolver()
	ctx := context.Background()
	for _, bad := range []types.Contact{
		{Type: types.ContactPhone, Value: "abc123"},
		{Type: types.ContactPhone, Value: "12345"}, // too short
		{Type: types.ContactEmail, Value: "not-an-email"},
		{Type: types.ContactEmail, Value: "a@b"}, // no dot in domain
	} {
		if _, err := r.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: types.Scope{TenantID: "t1"}, Contact: bad}); gkerr.ReasonOf(err) != gkerr.ReasonValidationFailed {
			t.Errorf("contact %q should be VALIDATION_FAILED, got %v", bad.Value, err)
		}
	}
}

func TestTurnBalanceGrantConsume(t *testing.T) {
	store := memstore.New()
	ctx := context.Background()
	if _, err := store.GrantTurns(ctx, "t1", "p1", "camp1", 3); err != nil {
		t.Fatal(err)
	}
	bal, _ := store.GetTurnBalance(ctx, "t1", "p1", "camp1")
	if bal != 3 {
		t.Fatalf("balance = %d, want 3", bal)
	}
	for i := 0; i < 3; i++ {
		ok, _, _ := store.ConsumeTurn(ctx, "t1", "p1", "camp1")
		if !ok {
			t.Fatalf("consume %d should succeed", i)
		}
	}
	ok, _, _ := store.ConsumeTurn(ctx, "t1", "p1", "camp1")
	if ok {
		t.Error("consume past zero should fail")
	}
	// Different scope key is isolated.
	if bal, _ := store.GetTurnBalance(ctx, "t1", "p1", "camp2"); bal != 0 {
		t.Errorf("other scope balance = %d, want 0", bal)
	}
}
