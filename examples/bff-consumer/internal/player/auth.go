package player

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"math/big"
	"strconv"
	"sync"
	"time"

	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Authentication lives in the BFF, not Core. The BFF issues a challenge, verifies
// the submitted code/proof, then asks Core to resolve-or-create the player
// (PlayerService.ResolvePlayer) for the now-verified contact and mints the JWT
// itself. Core never sees a challenge or a token.
//
// The challenge store here is in-memory (single-instance, fine for a reference
// BFF). A multi-instance deployment would back it with Redis/DB — the seam is
// the small challengeStore type below.

const challengeTTL = 10 * time.Minute

type challenge struct {
	tenantID     string
	merchantID   string
	contactType  string // "phone" | "email"
	contactValue string
	method       string
	secret       string // expected code/proof ("" never matches)
	expiresAt    time.Time
}

type challengeStore struct {
	mu sync.Mutex
	m  map[string]challenge
}

func newChallengeStore() *challengeStore { return &challengeStore{m: map[string]challenge{}} }

func (s *challengeStore) put(id string, c challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = c
}

// take returns the challenge and removes it (single-use), or ok=false if absent.
func (s *challengeStore) take(id string) (challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[id]
	if ok {
		delete(s.m, id)
	}
	return c, ok
}

// issueSecret returns the secret to store and the dev code to optionally reveal,
// for the chosen method. code/otp → a 6-digit code; magic_link → an opaque
// token echoed back as proof; social → auto-verify (no code). Real providers
// (SMS OTP, email link, OAuth) would replace this with a network call.
func issueSecret(method string) (secret, devCode string) {
	switch method {
	case "magic_link":
		tok := randomHex(16)
		return tok, tok
	case "social":
		return "", "" // auto-verify; nothing to send
	default: // code / otp
		code := randomCode()
		return code, code
	}
}

// checkSecret validates the submitted code/proof against the stored secret.
// social auto-verifies (empty stored secret); the others require a constant-time
// match against a non-empty secret.
func checkSecret(method, secret, submitted string) bool {
	if method == "social" {
		return true
	}
	return secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(submitted)) == 1
}

func randomCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "000000"
	}
	return strconv.Itoa(100000 + int(n.Int64()))
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// contactTypeEnum maps the BFF's contact-type token to the Core enum used by
// ResolvePlayer (UNSPECIFIED for an unknown token — Core rejects it).
func contactTypeEnum(t string) gamev1.ContactType {
	switch t {
	case "phone":
		return gamev1.ContactType_CONTACT_TYPE_PHONE
	case "email":
		return gamev1.ContactType_CONTACT_TYPE_EMAIL
	default:
		return gamev1.ContactType_CONTACT_TYPE_UNSPECIFIED
	}
}
