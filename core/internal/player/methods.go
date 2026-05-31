package player

import (
	"crypto/subtle"
	"strconv"

	"github.com/muse/gamekit/ports"
)

// Method verifies a contact for one auth channel. Issue mints the server-side
// secret to store (and a dev_code to optionally reveal in dev mode); Check
// validates what the client submitted at verify time. Real providers (SMS OTP,
// email magic-link, OAuth social) implement this same seam — swap them in via
// Authenticator.Register without touching the flow.
type Method interface {
	Issue(rand ports.RandSource, ids ports.IDGen) (secret, devCode string)
	Check(secret, submitted string) bool
}

// DevCodeMethod is the dev/stub for code + otp: a random 6-digit code, revealed
// in dev mode so e2e can complete without an SMS gateway. Swap for a real OTP
// provider in production.
type DevCodeMethod struct{}

func (DevCodeMethod) Issue(rand ports.RandSource, _ ports.IDGen) (string, string) {
	code := strconv.Itoa(100000 + rand.Intn(900000)) // 6 digits
	return code, code
}

func (DevCodeMethod) Check(secret, submitted string) bool {
	return secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(submitted)) == 1
}

// DevLinkMethod is the dev/stub for magic_link: an opaque token the client
// echoes back as the proof. In production the token would be emailed as a link.
type DevLinkMethod struct{}

func (DevLinkMethod) Issue(_ ports.RandSource, ids ports.IDGen) (string, string) {
	tok := ids.NewID("magic")
	return tok, tok
}

func (DevLinkMethod) Check(secret, submitted string) bool {
	return secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(submitted)) == 1
}

// SocialStubMethod is the dev/stub for social login: it auto-verifies, trusting
// that the contact was supplied by a social provider. Replace with a real OAuth
// provider that validates the proof token and derives the verified contact.
type SocialStubMethod struct{}

func (SocialStubMethod) Issue(ports.RandSource, ports.IDGen) (string, string) {
	return "social", "" // no code to reveal; verify auto-succeeds
}

func (SocialStubMethod) Check(_, _ string) bool { return true }
