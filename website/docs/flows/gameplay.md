---
title: Gameplay (start → play)
sidebar_position: 1
---

# Gameplay: `start` → `play`

The core loop. A player opens the widget, the server mints a session, the player acts, the server
decides the outcome and atomically commits it.

## Start

```mermaid
sequenceDiagram
  autonumber
  participant W as Widget
  participant C as Consumer BFF
  participant Core as Core (engine)
  participant R as Redis
  participant DB as SQL

  W->>C: POST /api/v1/games/{id}/start
  C->>Core: StartGame(scope, game_id, player_id) [gRPC]
  Core->>DB: load game config
  Core->>Core: run seed_generator (e.g. drop_sequence)
  Core->>R: store session (seed, expires_at, secret) — TTL
  Core->>DB: write durable session row (audit)
  Core-->>C: { session_id, seed_data, expires_at }
  C-->>W: 200 envelope { data: { session_id, seed_data, expires_at } }
```

The `seed_data` is what the widget needs to render (e.g. the catch sequence for a gift-catcher).
For a spin wheel it's `null`.

## Play

```mermaid
sequenceDiagram
  autonumber
  participant W as Widget
  participant C as Consumer BFF
  participant Core as Core (engine)
  participant R as Redis
  participant DB as SQL

  W->>C: POST /games/{id}/play { session_id, payload }
  Note over C: rate-limit (per player/IP) · attach trace_id
  C->>Core: Play(scope, game_id, session_id, player_id, payload) [gRPC]

  Core->>R: idempotency key seen? (return cached result if so)
  Core->>R: load + consume session (reject if expired/consumed/unknown)
  Core->>Core: validator.Validate (anti-cheat)
  Core->>Core: reward_handler.Evaluate → rewards[]

  rect rgb(237,233,254)
    Note over Core,DB: single transaction
    Core->>DB: eligibility (turns / caps) in-txn
    Core->>DB: atomic stock deduct per prize (remaining > 0)
    Core->>DB: insert reward records
    Core->>DB: credit wallet (points / lucky_item)
    Core->>DB: enqueue fulfillment_tasks (async channels)
    Core->>DB: insert immutable play_history (+ trace_id)
  end

  Core->>R: store idempotency result
  Core-->>C: { play_id, rewards[], metadata }
  Core--)Core: emit play_completed / prize_won (best-effort)
  C-->>W: 200 envelope { data: reward_result }
```

### What can go wrong (and the error you get)

| Situation | `reason` |
|---|---|
| Session expired / already played | `SESSION_EXPIRED` / `SESSION_CONSUMED` |
| No turns left / outside window | `OUT_OF_TURNS` / `GAME_NOT_ACTIVE` |
| Prize stock exhausted | `PRIZE_OUT_OF_STOCK` |
| Anti-cheat rejected the payload | `CHEAT_DETECTED` |
| Too many requests | `RATE_LIMITED` (with `Retry-After`) |

See the full table in the [Error reference](../reference/errors.md).

## Why it's safe under load

Stock is a **conditional atomic update** (`remaining = remaining - 1 WHERE remaining > 0`) inside
the transaction — not a lock. N concurrent plays on a 1-unit prize yield exactly one winner; the
rest fall through to no-win. The idempotency key makes a retried winning `Play` return the same
result instead of double-awarding.
