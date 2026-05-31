// Package auth is the BFF authentication seam. A player JWT (issued by Core's
// VerifyAuth) is the primary source of the caller's tenant/merchant/player/
// identity; the Bearer middleware verifies it and stashes the claims in the
// request context. For backward compatibility (and admin/widget callers without
// a player token), Scope()/PlayerID() fall back to the X-Tenant-Id /
// X-Merchant-Id / X-Player-Id headers when no verified claims are present.
package auth

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/muse/pkg/token"
)

type claimsKey struct{}

// Claims is the verified player identity attached by the Bearer middleware.
type Claims = token.Claims

// withClaims stores verified claims on the context.
func withClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// ClaimsFrom returns the verified claims, or nil if the request was unauthenticated.
func ClaimsFrom(r *http.Request) *Claims {
	if c, ok := r.Context().Value(claimsKey{}).(*Claims); ok {
		return c
	}
	return nil
}

// Scope returns the caller's tenant and merchant ids — from verified JWT claims
// when present, else the X-Tenant-Id / X-Merchant-Id headers.
func Scope(r *http.Request) (tenantID, merchantID string) {
	if c := ClaimsFrom(r); c != nil {
		return c.TenantID, c.MerchantID
	}
	return r.Header.Get("X-Tenant-Id"), r.Header.Get("X-Merchant-Id")
}

// PlayerID returns the caller's player id — from the verified JWT subject when
// present, else the X-Player-Id header.
func PlayerID(r *http.Request) string {
	if c := ClaimsFrom(r); c != nil {
		return c.PlayerID
	}
	return r.Header.Get("X-Player-Id")
}

// IdentityID returns the caller's global identity id from verified claims (""
// when unauthenticated or header-only).
func IdentityID(r *http.Request) string {
	if c := ClaimsFrom(r); c != nil {
		return c.IdentityID
	}
	return ""
}

// Roles returns the caller's authorization roles — from verified JWT claims when
// present, else the comma-separated X-Roles header (a dev/e2e seam mirroring the
// X-Tenant-Id fallback; in production the admin JWT carries roles).
func Roles(r *http.Request) []string {
	if c := ClaimsFrom(r); c != nil && len(c.Roles) > 0 {
		return c.Roles
	}
	if h := r.Header.Get("X-Roles"); h != "" {
		parts := strings.Split(h, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

// HasRole reports whether the caller holds any of the wanted roles.
func HasRole(r *http.Request, wanted ...string) bool {
	have := Roles(r)
	for _, w := range wanted {
		if slices.Contains(have, w) {
			return true
		}
	}
	return false
}

// BearerToken extracts the raw token from an Authorization: Bearer header.
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
