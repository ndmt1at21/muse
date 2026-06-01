package engine_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/memstore"
	"github.com/muse/gamekit/types"
)

// --- egg-catcher: score_to_tier + time_and_score_range ---

func eggGame() *types.Game {
	return &types.Game{
		ID:            "game_egg",
		Scope:         testScope,
		Name:          "Egg Catcher",
		Type:          "egg_catcher",
		SeedGenerator: "none",
		RewardHandler: "score_to_tier",
		Validator:     "time_and_score_range",
		Status:        types.StatusActive,
		Rules:         types.Rules{MaxPlaysPerUser: 5},
		HandlerConfig: json.RawMessage(`{
			"tiers": [
				{"min":0,"max":29,"prize_group":"t0"},
				{"min":30,"max":69,"prize_group":"t1"},
				{"min":70,"max":1000,"prize_group":"t2"}
			],
			"prize_groups": {
				"t1": [{"prize_id":"prize_small","probability":1.0}],
				"t2": [{"prize_id":"prize_big","probability":1.0}]
			}
		}`),
		ValidatorConfig: json.RawMessage(`{"min_duration_ms":2000,"max_duration_ms":120000,"max_score":150}`),
	}
}

func TestEggCatcherTierAward(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0))
	store.PutGame(eggGame())
	store.PutPrize(&types.Prize{ID: "prize_small", Scope: testScope, Name: "Small", Type: "voucher", Total: 10, Remaining: 10})
	store.PutPrize(&types.Prize{ID: "prize_big", Scope: testScope, Name: "Big", Type: "voucher", Total: 10, Remaining: 10})

	ctx := context.Background()
	// score 75 → tier t2 → prize_big
	start, _ := eng.Start(ctx, testScope, "game_egg", "p1")
	res, err := eng.Play(ctx, testScope, "game_egg", start.SessionID, "p1", json.RawMessage(`{"score":75,"duration_ms":8000}`), "")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if len(res.Rewards) != 1 || res.Rewards[0].PrizeID != "prize_big" {
		t.Fatalf("expected prize_big, got %+v", res.Rewards)
	}
	if res.Metadata["tier"] != "t2" {
		t.Errorf("expected tier t2, got %v", res.Metadata["tier"])
	}
}

func TestEggCatcherLowScoreNoReward(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0))
	store.PutGame(eggGame())

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_egg", "p1")
	// score 10 → tier t0 (no prize group) → no reward
	res, err := eng.Play(ctx, testScope, "game_egg", start.SessionID, "p1", json.RawMessage(`{"score":10,"duration_ms":8000}`), "")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if len(res.Rewards) != 0 {
		t.Errorf("expected no reward for low score, got %+v", res.Rewards)
	}
}

func TestEggCatcherCheatScoreCeiling(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0))
	store.PutGame(eggGame())

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_egg", "p1")
	_, err := eng.Play(ctx, testScope, "game_egg", start.SessionID, "p1", json.RawMessage(`{"score":9999,"duration_ms":8000}`), "")
	if gkerr.ReasonOf(err) != gkerr.ReasonCheatDetected {
		t.Fatalf("expected CHEAT_DETECTED for score over ceiling, got %v", err)
	}
}

func TestEggCatcherCheatTooFast(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0))
	store.PutGame(eggGame())

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_egg", "p1")
	_, err := eng.Play(ctx, testScope, "game_egg", start.SessionID, "p1", json.RawMessage(`{"score":50,"duration_ms":100}`), "")
	if gkerr.ReasonOf(err) != gkerr.ReasonCheatDetected {
		t.Fatalf("expected CHEAT_DETECTED for too-fast play, got %v", err)
	}
}

// A configured min_duration_ms is mandatory: a play that omits duration_ms or
// reports zero must not bypass the too-fast anti-cheat by dodging the field.
func TestEggCatcherCheatZeroOrMissingDuration(t *testing.T) {
	ctx := context.Background()
	for name, payload := range map[string]string{
		"omitted": `{"score":50}`,
		"zero":    `{"score":50,"duration_ms":0}`,
	} {
		t.Run(name, func(t *testing.T) {
			clk := &fixedClock{t: time.Now().UTC()}
			eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.0))
			store.PutGame(eggGame())

			start, _ := eng.Start(ctx, testScope, "game_egg", "p1")
			_, err := eng.Play(ctx, testScope, "game_egg", start.SessionID, "p1", json.RawMessage(payload), "")
			if gkerr.ReasonOf(err) != gkerr.ReasonCheatDetected {
				t.Fatalf("expected CHEAT_DETECTED for %s duration, got %v", name, err)
			}
		})
	}
}

// --- gift-catcher: drop_sequence + collect_items + drop_plan ---

func giftGame() *types.Game {
	return &types.Game{
		ID:            "game_gift",
		Scope:         testScope,
		Name:          "Gift Catcher",
		Type:          "gift_catcher",
		SeedGenerator: "drop_sequence",
		RewardHandler: "collect_items",
		Validator:     "drop_plan",
		Status:        types.StatusActive,
		Rules:         types.Rules{MaxPlaysPerUser: 5},
		HandlerConfig: json.RawMessage(`{
			"drops": [
				{"type":"voucher_50k","prize_id":"prize_v50","frequency":4,"max_catchable":2},
				{"type":"coin","prize_id":"prize_coin","frequency":3,"max_catchable":3}
			],
			"total_items": 12,
			"interval_ms": 500
		}`),
	}
}

func seedSequence(t *testing.T, store *memstore.Store, sessionID string) types.DropSequence {
	t.Helper()
	s, err := store.GetSession(context.Background(), testScope, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	var seq types.DropSequence
	if err := json.Unmarshal(s.SeedData, &seq); err != nil {
		t.Fatalf("decode seed: %v", err)
	}
	return seq
}

// catchN returns the first n drop_ids of the given type from the sequence.
func catchN(seq types.DropSequence, typ string, n int) []string {
	var out []string
	for _, d := range seq.Drops {
		if d.Type == typ && len(out) < n {
			out = append(out, d.ID)
		}
	}
	return out
}

func TestGiftCatcherCollectAndAward(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.1, 0.7, 0.3, 0.9, 0.5))
	store.PutGame(giftGame())
	store.PutPrize(&types.Prize{ID: "prize_v50", Scope: testScope, Name: "Voucher 50K", Type: "voucher", Total: 10, Remaining: 10})
	store.PutPrize(&types.Prize{ID: "prize_coin", Scope: testScope, Name: "Coin", Type: "points", Total: 100, Remaining: 100})

	ctx := context.Background()
	start, err := eng.Start(ctx, testScope, "game_gift", "p1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	seq := seedSequence(t, store, start.SessionID)
	if seq.TotalItems != 12 {
		t.Fatalf("expected 12 drops, got %d", seq.TotalItems)
	}

	// Catch 2 vouchers (== cap) and 1 coin.
	caught := append(catchN(seq, "voucher_50k", 2), catchN(seq, "coin", 1)...)
	body, _ := json.Marshal(map[string]any{"caught_items": caught})

	res, err := eng.Play(ctx, testScope, "game_gift", start.SessionID, "p1", body, "")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	// 2 vouchers + 1 coin = 3 reward units.
	if len(res.Rewards) != 3 {
		t.Fatalf("expected 3 reward units, got %d (%+v)", len(res.Rewards), res.Rewards)
	}
	if res.Metadata["total_caught"] != 3 {
		t.Errorf("expected total_caught 3, got %v", res.Metadata["total_caught"])
	}
	// Stock: vouchers (a stock prize) deduct 10→8. Coins are `points` — a wallet
	// currency (Phase 8): the engine routes them to the WalletRouter, never the
	// stock ledger, so coin Remaining is untouched (and uncredited here, as this
	// test wires no WalletRouter).
	v, _ := store.GetPrize(ctx, testScope, "prize_v50")
	c, _ := store.GetPrize(ctx, testScope, "prize_coin")
	if v.Remaining != 8 || c.Remaining != 100 {
		t.Errorf("expected voucher 8 / coin 100, got %d / %d", v.Remaining, c.Remaining)
	}
}

func TestGiftCatcherRejectsUnknownDrop(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.1, 0.7, 0.3))
	store.PutGame(giftGame())

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_gift", "p1")
	body, _ := json.Marshal(map[string]any{"caught_items": []string{"d_does_not_exist"}})
	_, err := eng.Play(ctx, testScope, "game_gift", start.SessionID, "p1", body, "")
	if gkerr.ReasonOf(err) != gkerr.ReasonCheatDetected {
		t.Fatalf("expected CHEAT_DETECTED for unknown drop, got %v", err)
	}
}

func TestGiftCatcherRejectsOverCatch(t *testing.T) {
	clk := &fixedClock{t: time.Now().UTC()}
	eng, store, _ := buildEngine(t, clk, memstore.NewSeqRand(0.1, 0.7, 0.3))
	store.PutGame(giftGame())

	ctx := context.Background()
	start, _ := eng.Start(ctx, testScope, "game_gift", "p1")
	seq := seedSequence(t, store, start.SessionID)
	// voucher_50k cap is 2; try to claim 3 (frequency is 4 so 3 exist).
	caught := catchN(seq, "voucher_50k", 3)
	if len(caught) < 3 {
		t.Fatalf("need 3 voucher drops in seed, got %d", len(caught))
	}
	body, _ := json.Marshal(map[string]any{"caught_items": caught})
	_, err := eng.Play(ctx, testScope, "game_gift", start.SessionID, "p1", body, "")
	if gkerr.ReasonOf(err) != gkerr.ReasonCheatDetected {
		t.Fatalf("expected CHEAT_DETECTED for over-catch, got %v", err)
	}
}
