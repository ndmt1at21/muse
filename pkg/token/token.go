// Package token issues and verifies the player JWT (HS256). It is a small,
// dependency-free implementation shared by Core (which signs after auth) and
// bffkit (which verifies at the edge) — the only thing they must agree on is the
// HMAC secret. Claims carry the tenant/merchant/player/identity scope so the BFF
// resolves the caller without another round-trip to Core.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims are the player JWT payload (snake_case for interoperability).
type Claims struct {
	TenantID   string `json:"tenant_id"`
	MerchantID string `json:"merchant_id,omitempty"`
	PlayerID   string `json:"player_id"`
	IdentityID string `json:"identity_id"`
	// Roles carry admin/staff authorization (e.g. admin, designer,
	// reward_manager). Empty for player tokens; the admin BFF guards routes on it.
	Roles     []string `json:"roles,omitempty"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
}

var (
	// ErrInvalid means the token is malformed, wrongly signed, or unsupported.
	ErrInvalid = errors.New("token: invalid")
	// ErrExpired means the token's exp is in the past.
	ErrExpired = errors.New("token: expired")
)

type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

var b64 = base64.RawURLEncoding

// Sign returns a signed HS256 JWT for the claims, set to expire ttl from now.
// `now` is injected so callers stay testable/deterministic.
func Sign(secret string, c Claims, now time.Time, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("token: empty secret")
	}
	c.IssuedAt = now.Unix()
	c.ExpiresAt = now.Add(ttl).Unix()
	h, err := json.Marshal(header{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	p, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signingInput := b64.EncodeToString(h) + "." + b64.EncodeToString(p)
	sig := sign(secret, signingInput)
	return signingInput + "." + sig, nil
}

// Verify checks the signature and expiry and returns the claims. `now` is
// injected for deterministic tests.
func Verify(secret, tok string, now time.Time) (*Claims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, ErrInvalid
	}
	var h header
	hb, err := b64.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &h) != nil || h.Alg != "HS256" {
		return nil, ErrInvalid
	}
	expected := sign(secret, parts[0]+"."+parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, ErrInvalid
	}
	pb, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalid
	}
	var c Claims
	if err := json.Unmarshal(pb, &c); err != nil {
		return nil, ErrInvalid
	}
	if c.ExpiresAt > 0 && now.Unix() >= c.ExpiresAt {
		return nil, ErrExpired
	}
	return &c, nil
}

func sign(secret, input string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(input))
	return b64.EncodeToString(mac.Sum(nil))
}
