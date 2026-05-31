package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muse/pkg/token"
)

func TestRolesFromHeaderAndClaims(t *testing.T) {
	// Dev header seam.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Roles", "admin, reward_manager ,")
	if got := Roles(r); len(got) != 2 || got[0] != "admin" || got[1] != "reward_manager" {
		t.Fatalf("header roles: %v", got)
	}

	// Verified claims take precedence.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("X-Roles", "designer")
	r2 = r2.WithContext(withClaims(r2.Context(), &token.Claims{Roles: []string{"admin"}}))
	if !HasRole(r2, "admin") || HasRole(r2, "designer") {
		t.Fatalf("claims roles should win over header: %v", Roles(r2))
	}
}

func TestRequireRole(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	guard := RequireRole("admin", "reward_manager")

	// No role → 403.
	rec := httptest.NewRecorder()
	guard(next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/games", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing role: want 403, got %d", rec.Code)
	}

	// Has an allowed role → passes.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/games", nil)
	req.Header.Set("X-Roles", "reward_manager")
	guard(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed role: want 200, got %d", rec.Code)
	}

	// Wrong role → 403.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/games", nil)
	req.Header.Set("X-Roles", "viewer")
	guard(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong role: want 403, got %d", rec.Code)
	}
}
