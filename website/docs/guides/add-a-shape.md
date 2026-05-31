---
title: Add a shape (one handler)
sidebar_position: 3
---

# Add a new game shape

When a mechanic doesn't fit any built-in handler, you add one — a small Go type that implements a
registry interface. **This is the only time gameplay needs code, and the engine core is never
touched.**

```mermaid
flowchart LR
  impl["implement RewardHandler<br/>(pure: deps + config + payload → rewards)"] --> reg["RegisterHandler(\"my_shape\", …)"]
  reg --> cfg["games can now use<br/>reward_handler: \"my_shape\""]
```

## The three interfaces

```go
// in package gamekit
type HandlerDeps struct { /* Prizes, Rand, Clock, … ports the handler may use */ }

type RewardHandler interface {
    Evaluate(ctx context.Context, deps HandlerDeps, game *types.Game, seed types.SeedData, payload types.Payload) (types.RewardResult, error)
}
type SeedGenerator interface {
    Generate(ctx context.Context, deps HandlerDeps, game *types.Game, session *types.Session) (types.SeedData, error)
}
type Validator interface {
    Validate(ctx context.Context, game *types.Game, session *types.Session, payload types.Payload) error
}
```

A handler is **pure logic over ports** — no DB, no HTTP. That's why it's unit-testable with
in-memory fakes and works identically embedded (Mode A) or hosted (Mode B).

## Example: a "highest-card-wins" handler

```go
package handlers

import (
    "context"
    "encoding/json"

    "github.com/muse/gamekit"
    "github.com/muse/gamekit/gkerr"
    "github.com/muse/gamekit/types"
)

type HighCard struct{}

type highCardConfig struct {
    WinPrizeID  string `json:"win_prize_id"`
    WinAtOrAbove int   `json:"win_at_or_above"` // 1..13
}

func (HighCard) Evaluate(ctx context.Context, deps gamekit.HandlerDeps, game *types.Game, seed types.SeedData, payload types.Payload) (types.RewardResult, error) {
    var cfg highCardConfig
    if err := json.Unmarshal(game.HandlerConfig, &cfg); err != nil {
        return types.RewardResult{}, gkerr.New(gkerr.ReasonValidationFailed, "bad high_card config").Wrap(err)
    }
    card := 1 + int(deps.Rand.Float64()*13) // server draws — never trust the client
    meta := map[string]any{"card": card}
    if card < cfg.WinAtOrAbove {
        return types.RewardResult{Metadata: meta}, nil // no-win
    }
    return types.RewardResult{
        Rewards:  []types.Reward{{PrizeID: cfg.WinPrizeID, Quantity: 1, Type: "voucher"}},
        Metadata: meta,
    }, nil
}
```

The engine handles everything around it: stock deduction for the returned prize, the reward record,
wallet routing, fulfillment, and history — uniformly.

## Register it

Add one line where the registry is built (`gamekit/std/Registry()`), or register on your own
registry when embedding:

```go
r.RegisterHandler("high_card", handlers.HighCard{})
```

Now any game can use it from config:

```jsonc
{ "type": "high_card", "reward_handler": "high_card", "seed_generator": "none", "validator": "basic",
  "handler_config": { "win_prize_id": "prize_x", "win_at_or_above": 11 } }
```

## Checklist

- [ ] Implement `RewardHandler` (and a `SeedGenerator` / `Validator` if the shape needs server seeds
      or custom anti-cheat).
- [ ] Keep it **pure** — depend only on `HandlerDeps` / config / payload.
- [ ] Return `RewardResult{ Rewards, Metadata }`; let the engine do stock/fulfillment/history.
- [ ] Register it under a stable key.
- [ ] Unit-test with in-memory fakes (no DB needed) — see `gamekit/engine/*_test.go` for the pattern.

:::tip Anti-cheat lives in the Validator
If the shape accepts client inputs (score, caught items), add a `Validator` so the server can reject
impossible inputs — see [Anti-cheat](../concepts/anti-cheat.md).
:::
