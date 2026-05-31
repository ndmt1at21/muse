package engine_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muse/gamekit/engine"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/memstore"
	"github.com/muse/gamekit/std"
	"github.com/muse/gamekit/types"
)

// fixedClock lets tests control time.
type fixedClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

var testScope = types.Scope{TenantID: "tenant_1", MerchantID: "merchant_1"}

// buildEngine wires the engine against the in-memory store with deterministic
// clock/rand/ids — no DB, no Redis. This is the Mode A proof: the SDK runs standalone.
func buildEngine(t *testing.T, clk *fixedClock, rnd *memstore.SeqRand) (*engine.Engine, *memstore.Store, *memstore.EventSink) {
	t.Helper()
	store := memstore.New()
	sink := &memstore.EventSink{}
	eng := engine.New(engine.Deps{
		Registry: std.Registry(),
		Games:    store,
		Prizes:   store,
		Sessions: store,
		History:  store,
		Tx:       memstore.TxRunner{},
		Events:   sink,
		Clock:    clk,
		Rand:     rnd,
		IDs:      &memstore.SeqIDGen{},
	}, engine.Config{SessionTTL: 5 * time.Minute})
	return eng, store, sink
}

func spinGame(handlerConfig string) *types.Game {
	return &types.Game{
		ID:            "game_spin",
		Scope:         testScope,
		Name:          "Lucky Spin",
		Type:          "spin_wheel",
		SeedGenerator: "none",
		RewardHandler: "probability",
		Validator:     "basic",
		Status:        types.StatusActive,
		Rules:         types.Rules{MaxPlaysPerUser: 3},
		HandlerConfig: json.RawMessage(handlerConfig),
	}
}

func TestSpinWheelHappyPath(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)}
	// rnd 0.5 -> with weights [voucher 0.1, nothing 0.9] over total 1.0, 0.5 lands in "nothing".
	// rnd 0.05 -> lands in voucher. We'll test the win.
	rnd := memstore.NewSeqRand(0.05)
	eng, store, sink := buildEngine(t, clk, rnd)

	store.PutGame(spinGame(`{"prizes":[{"prize_id":"prize_v100","probability":0.1,"slot_index":0},{"prize_id":"","probability":0.9,"slot_index":1}]}`))
	store.PutPrize(&types.Prize{ID: "prize_v100", Scope: testScope, Name: "Voucher 100K", Type: "voucher", Value: 100000, Total: 5, Remaining: 5})

	ctx := context.Background()
	start, err := eng.Start(ctx, testScope, "game_spin", "player_1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if start.SessionID == "" {
		t.Fatal("expected session id")
	}

	res, err := eng.Play(ctx, testScope, "game_spin", start.SessionID, "player_1", json.RawMessage(`{}`), "trace_abc")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if len(res.Rewards) != 1 || res.Rewards[0].PrizeID != "prize_v100" {
		t.Fatalf("expected voucher reward, got %+v", res.Rewards)
	}
	if res.Metadata["slot_index"] != 0 {
		t.Errorf("expected slot_index 0, got %v", res.Metadata["slot_index"])
	}

	// Stock decremented.
	p, _ := store.GetPrize(ctx, testScope, "prize_v100")
	if p.Remaining != 4 {
		t.Errorf("expected remaining 4, got %d", p.Remaining)
	}
	// Events emitted.
	if sink.Count("play_completed") != 1 || sink.Count("prize_won") != 1 {
		t.Errorf("expected play_completed + prize_won events, got %+v", sink.Events)
	}
}

func TestSessionSingleUse(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0))
	store.PutGame(spinGame(`{"prizes":[{"prize_id":"","probability":1.0,"slot_index":0}]}`))

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_spin", "player_1")
	if _, err := eng.Play(ctx, testScope, "game_spin", start.SessionID, "player_1", json.RawMessage(`{}`), ""); err != nil {
		t.Fatalf("first play: %v", err)
	}
	_, err := eng.Play(ctx, testScope, "game_spin", start.SessionID, "player_1", json.RawMessage(`{}`), "")
	if gkerr.ReasonOf(err) != gkerr.ReasonSessionConsumed {
		t.Fatalf("expected SESSION_CONSUMED, got %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0))
	store.PutGame(spinGame(`{"prizes":[{"prize_id":"","probability":1.0,"slot_index":0}]}`))

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_spin", "player_1")
	clk.advance(10 * time.Minute) // past the 5m TTL
	_, err := eng.Play(ctx, testScope, "game_spin", start.SessionID, "player_1", json.RawMessage(`{}`), "")
	if gkerr.ReasonOf(err) != gkerr.ReasonSessionExpired {
		t.Fatalf("expected SESSION_EXPIRED, got %v", err)
	}
}

func TestOutOfTurns(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0))
	g := spinGame(`{"prizes":[{"prize_id":"","probability":1.0,"slot_index":0}]}`)
	g.Rules.MaxPlaysPerUser = 1
	store.PutGame(g)

	ctx := context.Background()
	s1, _ := eng.Start(ctx, testScope, "game_spin", "player_1")
	if _, err := eng.Play(ctx, testScope, "game_spin", s1.SessionID, "player_1", json.RawMessage(`{}`), ""); err != nil {
		t.Fatalf("play 1: %v", err)
	}
	s2, _ := eng.Start(ctx, testScope, "game_spin", "player_1")
	_, err := eng.Play(ctx, testScope, "game_spin", s2.SessionID, "player_1", json.RawMessage(`{}`), "")
	if gkerr.ReasonOf(err) != gkerr.ReasonOutOfTurns {
		t.Fatalf("expected OUT_OF_TURNS, got %v", err)
	}

	el, err := eng.Eligibility(ctx, testScope, "game_spin", "player_1")
	if err != nil {
		t.Fatalf("Eligibility: %v", err)
	}
	if el.CanPlay {
		t.Errorf("expected can_play=false")
	}
}

// TestConcurrentStockNoOverIssue fires N parallel plays at a prize with limited
// stock and asserts wins == stock (never more) — the atomic-deduct guarantee.
func TestConcurrentStockNoOverIssue(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0)) // always picks slot 0 (the prize)

	const stock = 10
	const players = 100
	g := spinGame(`{"prizes":[{"prize_id":"prize_ltd","probability":1.0,"slot_index":0}]}`)
	g.Rules = types.Rules{} // unlimited turns so stock is the only limiter
	store.PutGame(g)
	store.PutPrize(&types.Prize{ID: "prize_ltd", Scope: testScope, Name: "Limited", Type: "voucher", Total: stock, Remaining: stock})

	ctx := context.Background()
	var wins, outOfStock int64
	var wg sync.WaitGroup
	for i := 0; i < players; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start, err := eng.Start(ctx, testScope, "game_spin", "player_x")
			if err != nil {
				return
			}
			_, err = eng.Play(ctx, testScope, "game_spin", start.SessionID, "player_x", json.RawMessage(`{}`), "")
			switch gkerr.ReasonOf(err) {
			case "":
				atomic.AddInt64(&wins, 1)
			case gkerr.ReasonPrizeOutOfStock:
				atomic.AddInt64(&outOfStock, 1)
			}
		}()
	}
	wg.Wait()

	if wins != stock {
		t.Errorf("expected exactly %d wins, got %d (out_of_stock=%d)", stock, wins, outOfStock)
	}
	p, _ := store.GetPrize(ctx, testScope, "prize_ltd")
	if p.Remaining != 0 {
		t.Errorf("expected remaining 0, got %d", p.Remaining)
	}
}
