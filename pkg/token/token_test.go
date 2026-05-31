package token

import (
	"testing"
	"time"
)

func TestSignVerifyRoundtrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tok, err := Sign("secret", Claims{TenantID: "t1", PlayerID: "p1", IdentityID: "i1"}, now, time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	c, err := Verify("secret", tok, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.TenantID != "t1" || c.PlayerID != "p1" || c.IdentityID != "i1" {
		t.Errorf("claims mismatch: %+v", c)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tok, _ := Sign("secret", Claims{TenantID: "t1", PlayerID: "p1"}, now, time.Hour)
	if _, err := Verify("other", tok, now); err != ErrInvalid {
		t.Errorf("wrong secret should be ErrInvalid, got %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tok, _ := Sign("secret", Claims{TenantID: "t1", PlayerID: "p1"}, now, time.Minute)
	if _, err := Verify("secret", tok, now.Add(2*time.Minute)); err != ErrExpired {
		t.Errorf("expired token should be ErrExpired, got %v", err)
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tok, _ := Sign("secret", Claims{TenantID: "t1", PlayerID: "p1"}, now, time.Hour)
	if _, err := Verify("secret", tok+"x", now); err != ErrInvalid {
		t.Errorf("tampered token should be ErrInvalid, got %v", err)
	}
	if _, err := Verify("secret", "not.a.jwt.token", now); err != ErrInvalid {
		t.Errorf("malformed token should be ErrInvalid, got %v", err)
	}
}
