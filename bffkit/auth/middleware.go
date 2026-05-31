package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/muse/bffkit/envelope"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
	"github.com/muse/pkg/token"
)

// Verifier verifies a player JWT against the shared secret. now is injected so
// the middleware stays testable; in production it is time.Now.
type Verifier struct {
	secret string
	now    func() time.Time
}

// NewVerifier builds a Verifier. An empty secret disables verification (the
// middleware becomes a no-op pass-through — useful for header-only dev/e2e).
func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: secret, now: time.Now}
}

// Bearer is a "soft" auth middleware: if a Bearer token is present (and the
// verifier is enabled), it verifies it and attaches the claims to the context;
// an INVALID/expired token is rejected with 401. A missing token passes through
// unauthenticated — route-level guards (RequirePlayer) enforce where a player is
// actually required. This keeps public/widget endpoints and legacy header-based
// callers working on the same router.
func (v *Verifier) Bearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := BearerToken(r)
		if raw == "" || v.secret == "" {
			next.ServeHTTP(w, r)
			return
		}
		claims, err := token.Verify(v.secret, raw, v.now())
		if err != nil {
			envelope.WriteError(w, traceID(r), unauthenticated("invalid or expired token"))
			return
		}
		next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
	})
}

// RequirePlayer is a route guard that 401s unless a verified player JWT is
// present. Mount it on player-only routes. (Header-only callers without a token
// are rejected — use it only where a real player token is mandatory.)
func RequirePlayer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := ClaimsFrom(r); c == nil || c.PlayerID == "" {
			envelope.WriteError(w, traceID(r), unauthenticated("player authentication required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole is a route guard for the admin BFF: it 403s unless the caller
// holds at least one of the allowed roles (admin, designer, reward_manager).
// Roles come from a verified admin JWT, or the X-Roles dev header (see Roles).
func RequireRole(allowed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasRole(r, allowed...) {
				envelope.WriteError(w, traceID(r), permissionDenied("requires role: "+strings.Join(allowed, ", ")))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func unauthenticated(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonUnauthenticated, msg)).Err()
}

func permissionDenied(msg string) error {
	return apierr.FromDomainError(gkerr.New(gkerr.ReasonPermissionDenied, msg)).Err()
}

// traceID reads the trace id set by the trace middleware, without importing it
// (avoids an import cycle through bffkit/middleware → envelope).
func traceID(r *http.Request) string {
	return r.Header.Get("X-Trace-Id")
}
