package memstore

import (
	"context"
	"sync"

	"github.com/muse/gamekit/ports"
)

// TxRunner is a no-op transaction runner: the in-memory store has no real
// transactions, so it just runs fn. (Atomicity in tests comes from the Store
// mutex on each operation, which is sufficient for the concurrency test.)
type TxRunner struct{}

func (TxRunner) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// EventSink collects emitted events for assertion in tests.
type EventSink struct {
	mu     sync.Mutex
	Events []ports.Event
}

func (s *EventSink) Emit(ctx context.Context, evt ports.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, evt)
}

// Count returns how many events of a given type were emitted.
func (s *EventSink) Count(typ string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.Events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// SeqIDGen mints sequential, deterministic IDs ("<prefix>_<n>") for tests.
type SeqIDGen struct {
	mu sync.Mutex
	n  int
}

func (g *SeqIDGen) NewID(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return prefix + "_" + itoa(g.n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// SeqRand is a deterministic RandSource for tests: Float64 cycles through a
// fixed sequence of values, Intn returns sequential values mod n.
type SeqRand struct {
	mu     sync.Mutex
	floats []float64
	i      int
	n      int
}

// NewSeqRand returns a deterministic rand cycling through floats.
func NewSeqRand(floats ...float64) *SeqRand {
	if len(floats) == 0 {
		floats = []float64{0.0}
	}
	return &SeqRand{floats: floats}
}

func (r *SeqRand) Float64() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.floats[r.i%len(r.floats)]
	r.i++
	return v
}

func (r *SeqRand) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.n % n
	r.n++
	return v
}
