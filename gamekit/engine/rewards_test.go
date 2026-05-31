package engine_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/muse/gamekit/engine"
	"github.com/muse/gamekit/memstore"
	"github.com/muse/gamekit/std"
	"github.com/muse/gamekit/types"
)

// buildRewardEngine wires the engine WITH a RewardStore (memstore) so reward
// records, per-user caps, and code assignment are exercised — the Phase 3 path.
func buildRewardEngine(t *testing.T, clk *fixedClock) (*engine.Engine, *memstore.Store) {
	t.Helper()
	store := memstore.New()
	eng := engine.New(engine.Deps{
		Registry: std.Registry(),
		Games:    store, Prizes: store, Sessions: store, History: store, Rewards: store,
		Fulfill: store,
		Tx:      memstore.TxRunner{}, Events: &memstore.EventSink{},
		Clock: clk, Rand: memstore.NewSeqRand(0.0), IDs: &memstore.SeqIDGen{},
	}, engine.Config{SessionTTL: 5 * time.Minute})
	return eng, store
}

func alwaysWinGame() *types.Game {
	g := spinGame(`{"prizes":[{"prize_id":"prize_v","probability":1.0,"slot_index":0}]}`)
	g.Rules = types.Rules{} // unlimited turns; the prize cap is what we test
	return g
}

func TestRewardRecordCreatedOnWin(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store := buildRewardEngine(t, clk)
	store.PutGame(alwaysWinGame())
	store.PutPrize(&types.Prize{
		ID: "prize_v", Scope: testScope, Name: "Voucher", Type: "voucher", Total: 5, Remaining: 5,
		Fulfillment: types.PrizeFulfillment{RedemptionMode: types.RedemptionOnClaim},
	})

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_spin", "p1")
	res, err := eng.Play(ctx, testScope, "game_spin", start.SessionID, "p1", json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if len(res.Rewards) != 1 || res.Rewards[0].RewardID == "" {
		t.Fatalf("expected one reward with a reward_id, got %+v", res.Rewards)
	}
	recs := store.Rewards()
	if len(recs) != 1 || recs[0].Status != types.RewardWon {
		t.Fatalf("expected one WON reward record, got %+v", recs)
	}
}

func TestInstantRedemptionFulfilledImmediately(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store := buildRewardEngine(t, clk)
	store.PutGame(alwaysWinGame())
	store.PutPrize(&types.Prize{
		ID: "prize_v", Scope: testScope, Name: "Voucher", Type: "voucher", Total: 5, Remaining: 5,
		Fulfillment: types.PrizeFulfillment{RedemptionMode: types.RedemptionInstant, Method: "code"},
	})
	store.PutCodes(testScope, "prize_v", "CODE-A", "CODE-B")

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_spin", "p1")
	res, _ := eng.Play(ctx, testScope, "game_spin", start.SessionID, "p1", json.RawMessage(`{}`), "")
	if res.Rewards[0].Code != "CODE-A" {
		t.Errorf("expected assigned code CODE-A, got %q", res.Rewards[0].Code)
	}
	if recs := store.Rewards(); recs[0].Status != types.RewardFulfilled || recs[0].FulfilledAt == nil {
		t.Errorf("expected instant reward FULFILLED with timestamp, got %+v", recs[0])
	}
}

func TestFulfillmentTaskEnqueuedForAsyncChannel(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store := buildRewardEngine(t, clk)
	store.PutGame(alwaysWinGame())
	store.PutPrize(&types.Prize{
		ID: "prize_v", Scope: testScope, Name: "Big Prize", Type: "physical", Total: 5, Remaining: 5,
		Fulfillment: types.PrizeFulfillment{
			RedemptionMode: types.RedemptionInstant,
			Channel:        types.ChannelExternalWorkflow,
			ChannelConfig:  json.RawMessage(`{"webhook_url":"https://n8n.test/hook"}`),
		},
	})

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_spin", "p1")
	res, err := eng.Play(ctx, testScope, "game_spin", start.SessionID, "p1", json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}

	// An async-delivery channel leaves the reward WON (not auto-fulfilled) and
	// enqueues exactly one pending outbox task wired to the reward + channel.
	if recs := store.Rewards(); len(recs) != 1 || recs[0].Status != types.RewardWon {
		t.Fatalf("expected one WON reward (delivery pending), got %+v", recs)
	}
	tasks := store.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("expected one outbox task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.Status != types.TaskPending {
		t.Errorf("task status = %q, want pending", task.Status)
	}
	if task.Channel != types.ChannelExternalWorkflow {
		t.Errorf("task channel = %q, want external_workflow", task.Channel)
	}
	if task.RewardID == "" || task.RewardID != res.Rewards[0].RewardID {
		t.Errorf("task reward_id %q should match reward %q", task.RewardID, res.Rewards[0].RewardID)
	}
	if task.PrizeID != "prize_v" || task.PlayerID != "p1" {
		t.Errorf("task not bound to prize/player: %+v", task)
	}
}

func TestVoucherCodeChannelEnqueuesNoTask(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store := buildRewardEngine(t, clk)
	store.PutGame(alwaysWinGame())
	store.PutPrize(&types.Prize{
		ID: "prize_v", Scope: testScope, Name: "Voucher", Type: "voucher", Total: 5, Remaining: 5,
		Fulfillment: types.PrizeFulfillment{RedemptionMode: types.RedemptionInstant, Channel: types.ChannelVoucherCode},
	})
	store.PutCodes(testScope, "prize_v", "CODE-A")

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_spin", "p1")
	if _, err := eng.Play(ctx, testScope, "game_spin", start.SessionID, "p1", json.RawMessage(`{}`), ""); err != nil {
		t.Fatalf("Play: %v", err)
	}
	// In-app voucher delivery is immediate: fulfilled reward, no outbox task.
	if recs := store.Rewards(); recs[0].Status != types.RewardFulfilled {
		t.Errorf("voucher_code instant should be FULFILLED in-app, got %s", recs[0].Status)
	}
	if tasks := store.Tasks(); len(tasks) != 0 {
		t.Errorf("voucher_code should enqueue no task, got %d", len(tasks))
	}
}

func TestMaxPerUserCapDropsReward(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store := buildRewardEngine(t, clk)
	store.PutGame(alwaysWinGame())
	store.PutPrize(&types.Prize{
		ID: "prize_v", Scope: testScope, Name: "Voucher", Type: "voucher", Total: 10, Remaining: 10,
		Constraints: types.PrizeConstraints{MaxPerUser: 1},
	})

	ctx := context.Background()
	// First play wins.
	s1, _ := eng.Start(ctx, testScope, "game_spin", "p1")
	res1, _ := eng.Play(ctx, testScope, "game_spin", s1.SessionID, "p1", json.RawMessage(`{}`), "")
	if len(res1.Rewards) != 1 {
		t.Fatalf("first play should win, got %+v", res1.Rewards)
	}
	// Second play: handler picks it again, but the per-user cap drops it.
	s2, _ := eng.Start(ctx, testScope, "game_spin", "p1")
	res2, _ := eng.Play(ctx, testScope, "game_spin", s2.SessionID, "p1", json.RawMessage(`{}`), "")
	if len(res2.Rewards) != 0 {
		t.Fatalf("second play should be capped (no reward), got %+v", res2.Rewards)
	}
	// Stock only decremented once; a different player can still win.
	p, _ := store.GetPrize(ctx, testScope, "prize_v")
	if p.Remaining != 9 {
		t.Errorf("expected remaining 9 (one win), got %d", p.Remaining)
	}
	s3, _ := eng.Start(ctx, testScope, "game_spin", "p2")
	res3, _ := eng.Play(ctx, testScope, "game_spin", s3.SessionID, "p2", json.RawMessage(`{}`), "")
	if len(res3.Rewards) != 1 {
		t.Fatalf("different player should win, got %+v", res3.Rewards)
	}
}
