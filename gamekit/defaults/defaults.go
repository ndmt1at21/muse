// Package defaults provides production-ready implementations of the small
// utility ports (Clock, RandSource, IDGen). They use crypto/rand for IDs and
// math/rand for draws. Tests typically substitute deterministic fakes instead.
package defaults

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"math/rand/v2"
	"time"
)

// Clock returns the real wall clock.
type Clock struct{}

func (Clock) Now() time.Time { return time.Now() }

// Rand is a concurrency-safe PRNG suitable for prize draws (not cryptographic).
type Rand struct{}

func (Rand) Float64() float64 { return rand.Float64() }
func (Rand) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return rand.IntN(n)
}

// IDGen mints prefixed opaque IDs: "<prefix>_<16 hex bytes>".
type IDGen struct{}

func (IDGen) NewID(prefix string) string {
	var b [16]byte
	_, _ = cryptorand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}
