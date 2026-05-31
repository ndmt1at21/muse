// Command embed demonstrates Mode A: "use my logic, build your own API".
// It imports the pure gamekit SDK with the in-memory ports (memstore) — NO
// Core, NO BFF, NO database, NO Redis — and runs a full Start → Play. Swap
// memstore for github.com/muse/adapters/sqlstore + redisstore to get the
// batteries-included pg/mysql + Redis implementation behind the same ports.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/muse/gamekit/defaults"
	"github.com/muse/gamekit/engine"
	"github.com/muse/gamekit/memstore"
	"github.com/muse/gamekit/std"
	"github.com/muse/gamekit/types"
)

func main() {
	scope := types.Scope{TenantID: "tenant_demo", MerchantID: "merchant_demo"}
	store := memstore.New()

	// Seed a spin-wheel game + a prize, entirely in memory.
	store.PutGame(&types.Game{
		ID: "game_spin", Scope: scope, Name: "Lucky Spin", Type: "spin_wheel",
		SeedGenerator: "none", RewardHandler: "probability", Validator: "basic",
		Status: types.StatusActive, Rules: types.Rules{MaxPlaysPerUser: 5},
		HandlerConfig: json.RawMessage(`{"prizes":[
			{"prize_id":"prize_voucher","probability":0.5,"slot_index":0},
			{"prize_id":"","probability":0.5,"slot_index":1}]}`),
	})
	store.PutPrize(&types.Prize{
		ID: "prize_voucher", Scope: scope, Name: "Voucher 100K",
		Type: "voucher", Value: 100000, Total: 10, Remaining: 10,
	})

	eng := engine.New(engine.Deps{
		Registry: std.Registry(),
		Games:    store, Prizes: store, Sessions: store, History: store,
		Tx: memstore.TxRunner{}, Events: &memstore.EventSink{},
		Clock: defaults.Clock{}, Rand: defaults.Rand{}, IDs: defaults.IDGen{},
	}, engine.Config{SessionTTL: 5 * time.Minute})

	ctx := context.Background()
	start, err := eng.Start(ctx, scope, "game_spin", "player_alice")
	if err != nil {
		panic(err)
	}
	fmt.Printf("started session: %s (expires %s)\n", start.SessionID, start.ExpiresAt.Format(time.RFC3339))

	res, err := eng.Play(ctx, scope, "game_spin", start.SessionID, "player_alice", json.RawMessage(`{}`), "trace_demo")
	if err != nil {
		panic(err)
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Printf("play result:\n%s\n", out)
}
