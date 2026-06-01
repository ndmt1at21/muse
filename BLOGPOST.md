---
title: "Muse: a config-driven game engine where new games need zero backend code"
published: false
description: "How I built a generic backend for spin-the-wheel / scratch-card / catch-the-gift campaign games — where adding a new game is a JSON config, the server is authoritative against cheating, and the same engine embeds as a Go SDK or runs as a hosted API on Postgres or MySQL."
tags: go, gamedev, backend, architecture
cover_image: ""
canonical_url: ""
---

> **Repo:** https://github.com/ndmt1at21/muse · MIT licensed · Go
>
> *Draft — edit the front matter (cover image, canonical URL, `published: true`) before posting.*

Every marketing team eventually asks for the same thing: a **spin-the-wheel**, a
**scratch card**, a **catch-the-falling-gifts** mini-game to drive a campaign.
And every time, someone rebuilds the same backend from scratch — prize odds,
stock limits, anti-cheat, fulfillment — slightly differently, slightly buggily.

**Muse** is my answer: a **config-driven game engine** where adding a new game
of an existing *shape* needs **zero backend code** — just a JSON config. Adding
a genuinely new *shape* needs only a small handler; the engine core never
changes.

This post walks through the three ideas I think are worth stealing: the **shape
abstraction**, **server-authoritative anti-cheat**, and shipping the same engine
as **both an embeddable SDK and a hosted API**.

## 1. A game is three plug-ins, not a code branch

Instead of `if gameType == "spin_wheel" { ... }` sprinkled through the codebase,
every game is a combination of three registered plug-ins:

| Shape | `seed_generator` | `reward_handler` | `validator` |
|---|---|---|---|
| spin wheel / scratch card | `none` | `probability` | `basic` |
| egg catcher | `none` | `score_to_tier` | `time_and_score_range` |
| gift catcher | `drop_sequence` | `collect_items` | `drop_plan` |

- **seed generator** produces the server-authoritative setup at `start` (e.g.
  the exact sequence of falling gifts).
- **reward handler** decides the outcome at `play` (a weighted draw, a
  score→tier mapping, one prize per caught item).
- **validator** is the anti-cheat gate that runs *before* any reward is decided.

The engine's `Play` is shape-agnostic — it loads the session, runs the
validator, calls the handler, and commits the result in one transaction. So a
new game that reuses an existing combination is **pure config**:

```jsonc
// a new spin wheel — no code, no deploy
{
  "type": "spin_wheel",
  "reward_handler": "probability",
  "handler_config": { "prizes": [
    { "prize_id": "p_jackpot", "probability": 0.05, "slot_index": 0 },
    { "prize_id": "",          "probability": 0.95, "slot_index": 1 }   // no-win
  ] }
}
```

A *new shape* (say, a memory-match game) means registering one handler/seed/
validator — never touching the engine's transaction.

## 2. The client reports what it *did*, never what it *won*

The most interesting part is anti-cheat, because these games run in a browser
where the player controls the client. The rule throughout: **the client reports
what it did; the server decides what it won.**

Take the gift catcher. At `start`, the server generates and stores the full drop
sequence and hands it to the client to render. At `play`, the client submits the
`drop_id`s it caught — and the `drop_plan` validator checks them against the
*stored* sequence:

- caught a `drop_id` that isn't in the server's sequence → `CHEAT_DETECTED`
- claimed the same drop twice → `CHEAT_DETECTED`
- caught more of a type than its `max_catchable` cap → `CHEAT_DETECTED`

The score-based games get a ceiling and a minimum duration (a play that's
impossibly fast or reports no duration is rejected). The spin wheel ignores the
client payload entirely — the server draws the prize.

Underneath, three more guarantees make it solid under load:

- **Single-use sessions** — consuming a session is an atomic
  `UPDATE … WHERE consumed = false`; a replay finds 0 rows affected and fails.
- **Atomic stock** — `UPDATE prizes SET remaining = remaining - 1 WHERE remaining > 0`,
  so concurrent plays can't oversell the last unit (it's race-tested).
- **Idempotency** — a retried `play` with the same key returns the stored result
  instead of double-spending.

All of it is scoped by `(tenant_id, merchant_id)` on every query, so one tenant
can never touch another's sessions, stock, or history.

> A caveat I think is worth being honest about in any anti-cheat write-up: these
> are *trust-the-seed* / *trust-the-score* designs. The server guarantees you
> can't win **more than the rules allow** — but it can't prove you earned a score
> through real play. The economic protection is per-user caps + stock, not the
> game being "hard." Naming that boundary explicitly beats pretending it doesn't
> exist.

## 3. One engine, two ways to run it

The engine ships as a **pure SDK** (`github.com/muse/gamekit`) — stdlib-only, no
I/O, no transport, depends only on port interfaces. So you can consume Muse two
ways:

**Mode A — embed it.** Import the engine, register the built-in handlers, bring
your own storage (or use the provided Postgres/MySQL/Redis adapters), and call
`engine.Play(...)` in-process. No network, no proto.

**Mode B — run the hosted API.** Deploy `core`, which serves the full contract
over **gRPC and REST** (a grpc-gateway under `/api/v1`, wrapped in a uniform
`{ code, message, trace_id, data }` envelope). Core is **auth-agnostic** — it
trusts the caller to pass the tenant/merchant scope and only validates the
business object. You bring your own **BFF** for auth, RBAC, rate limiting, and
caching; a toolkit (`bffkit`) and two runnable reference BFFs (a public widget
and an admin dashboard) are included to copy.

The hosted flow is just two calls:

```bash
# 1. start a session (server issues the seed)
curl -s -X POST localhost:8080/api/v1/games/$GAME/start \
  -H "X-Tenant-Id: t1" -H "X-Player-Id: p1" -d '{}'

# 2. play (server validates + decides the reward, deducts stock atomically)
curl -s -X POST localhost:8080/api/v1/games/$GAME/play \
  -H "X-Tenant-Id: t1" -H "X-Player-Id: p1" \
  -d '{"session_id":"sess_…","payload":{}}'
```

Everything runs on **both Postgres and MySQL** behind one set of raw SQL queries
(no ORM), and every service exposes Prometheus metrics into a provisioned
Grafana dashboard.

## Bonus: presentation stays out of the engine

A recent addition I'm happy with: each game carries an **opaque `ui` JSON blob**
(background, theme colors, per-slot/per-item images). Core stores and returns it
but **never interprets** it — the front-end widget decodes it to theme itself per
game, and an admin can restyle a live game any time with no rebuild. The widget
fetches it from a **redacted** `/render` endpoint that returns the look but never
the odds. It's a small thing that keeps a clean line: **Core owns business, the
edge owns presentation.**

## Try it

```bash
git clone https://github.com/ndmt1at21/muse && cd muse
make embed        # Mode A: Start → Play, entirely in memory, no infra
make up && make seed && make e2e   # Mode B: the full hosted stack + a scripted play
```

The repo has a Docusaurus docs site (`make docs`) with the architecture,
sequence diagrams, and step-by-step *add-a-game* / *add-a-shape* guides.

If you build campaign games — or just like config-driven backends and
server-authoritative design — I'd love feedback. ⭐ the repo or open an issue:
**https://github.com/ndmt1at21/muse**
