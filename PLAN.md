# Game Service — Implementation Plan

## Context

We're building the **backend Game Service** for a dynamic website-builder platform. The
builder lets non-developers drag & configure components onto pages — including **game
components** (spin wheel, scratch card, egg-catcher, gift-catcher, quiz, collection games…).

The pain point driving the design: *adding a new game must not require new backend code every
time.* The conversation converged on a **config-driven generic game engine** — adding a game =
new FE component + a JSON config; the backend stays generic via a **handler/seed/validator
registry**. Around that engine sits a gamification-marketing platform (modeled on woay.vn):
Campaign, Quest, Reward, Player, Leaderboard, Inventory/Exchange, Integration.

### Confirmed decisions
- **Language**: Go for both tiers.
- **Topology**: 2 services — **Core** (domain, engine, persistence) + **BFF** (public widget +
  admin edge). Communicate via **gRPC** (typed protobuf contracts).
- **DB**: support **MySQL and PostgreSQL**, **no ORM** — `database/sql` + `sqlx`, raw SQL, thin
  dialect layer.
- **Scope**: plan covers the **full feature set**; build **runnable vertical slice first**, then
  expand context-by-context.
- **API conventions**: every REST response (success *and* error) uses the uniform envelope
  `{ code, message, trace_id, data }`; all JSON fields are **snake_case**; errors follow the
  **Google API error model** (canonical `google.rpc.Code` + structured `ErrorInfo`).

### Intended outcome
A generic, config-driven game backend where: (1) `start → play` works end-to-end with atomic
stock deduction and anti-cheat sessions, (2) new games of an existing *shape* need zero BE code
(just config), (3) new game *shapes* need only a small handler/validator, never engine changes.

---

## Architecture

Go monorepo (`go.work`), two deployables sharing protobuf contracts:

```
muse/
├── go.work
├── proto/                         # .proto contracts (buf)
│   └── game/v1/*.proto            # engine, campaign, reward, player, quest, leaderboard, inventory
├── pkg/                           # shared, service-agnostic
│   ├── gen/game/v1/               # buf-generated Go (gRPC + messages)
│   ├── dialect/                   # SQL dialect abstraction (pg/mysql placeholders, JSON type)
│   └── apierr/                    # error model + codes (maps to gRPC status & HTTP)
├── gamekit/                       # ★ ENGINE SDK — PURE domain logic, NO transport/DB/Redis deps
│   │                              #   importable standalone: "use my logic, build your own API"
│   ├── types/                     # domain models (Game, Prize, PlayResult, Session, Wallet, Group…)
│   ├── ports/                     # interfaces the logic depends on (consumer or our adapters implement):
│   │                              #   TenantStore, IdentityStore, GameStore, SessionStore,
│   │                              #   PrizeStore(atomic Deduct), WalletStore, GroupStore,
│   │                              #   LeaderboardStore, Cache, Locker, Clock, RandSource,
│   │                              #   EventSink, TxRunner  — all take a Scope{tenant_id,merchant_id}
│   ├── engine/                    # orchestrator Start/Play/Eligibility — depends ONLY on ports
│   ├── registry.go                # handler/seed/validator registries (pluggable)
│   ├── handlers/                  # probability, score_to_tier, collect_items, lucky_item
│   ├── seeds/                     # none, drop_sequence, random_pick
│   ├── validators/                # basic, time_and_score_range, drop_plan (anti-cheat)
│   ├── reward/ wallet/ group/     # business rules (stock policy, milestones, shared-goal split)
│   └── quest/ leaderboard/        # quest verify, ranking metric logic (storage via ports)
├── adapters/                      # ★ provided port IMPLEMENTATIONS (optional, batteries-included)
│   ├── sqlstore/                  # pg+mysql repos (no ORM, dialect) — *Store ports + atomic deduct
│   │   └── migrations/{postgres,mysql}/*.sql
│   ├── redisstore/                # sessions, cache, rate-limit, leaderboard ZSET, distributed lock
│   └── events/                    # redis pub/sub EventSink + transactional outbox dispatcher
├── core/                          # ★ API SERVICE = gamekit + adapters + gRPC transport (one composition)
│   ├── cmd/core/main.go           #   wires concrete adapters into gamekit, serves gRPC
│   ├── internal/
│   │   ├── grpcsvc/               # gRPC handlers: proto ↔ gamekit calls
│   │   ├── tenancy/               # tenant + merchant CRUD; identity resolve/link (phone/email)
│   │   ├── player/                # auth (phone/email OTP/magic-link/social/code) — service-level
│   │   ├── campaign/              # campaign aggregate + settings + channels (under merchant)
│   │   └── integration/           # webhook/gsheet/sms/zns/n8n adapter wiring
│   └── platform/                  # db pool, redis client, config, logging, health, OTel telemetry
├── bffkit/                        # SHARED BFF library (imported by both BFFs)
│   └── internal/
│       ├── coreclient/            # gRPC client wrapper to Core
│       ├── envelope/              # {code,message,trace_id,data} encode + gRPC↔HTTP error mapping
│       ├── middleware/            # JWT auth, rate limit, request validation, CORS, OTel
│       └── cache/                 # Redis/Dragonfly read-model cache (TTL + invalidation)
├── bff-consumer/                  # CONSUMER BFF (public internet): player + widget + realtime
│   ├── cmd/bff-consumer/main.go   #   /api/v1/{games,players,quests,leaderboards,wallet,public}, /ws, /sse
│   └── internal/{game,player,quest,leaderboard,wallet,group,match,public,ws}/
├── bff-admin/                     # ADMIN BFF (internal/VPN): dashboard + service callbacks
│   ├── cmd/bff-admin/main.go      #   /api/v1/admin/*, n8n /fulfillment/tasks/{id}/callback (HMAC)
│   └── internal/{game,prize,campaign,quest,leaderboard,integration,fulfillment}/
├── match/                         # (Phase 13) STATEFUL realtime-sync multiplayer service
│   ├── cmd/match/main.go          #   authoritative rooms + tick loop + bidirectional WS protocol
│   └── internal/{room,state,ws,matchmaking}/   # results → Core reward/leaderboard pipeline
├── deploy/
│   ├── docker-compose.yml         # postgres + mysql + dragonfly + core + bff-consumer + bff-admin
│   │                              #   + match + otel-collector + grafana + prometheus + tempo + loki
│   ├── grafana/                   # provisioned dashboards (RED, gameplay, stock, fulfillment) + alerts
│   ├── otel-collector.yaml        # receives OTLP → exports to tempo/prometheus/loki
│   └── Makefile / Taskfile        # buf generate, migrate, run, test
└── README.md
```

### Layering: engine SDK vs hosted API (two consumption modes)

The engine is shipped as a standalone **`gamekit` SDK** (pure Go, ports-and-adapters) so it can be
consumed two ways:

- **Mode A — embed the SDK (use the logic, bring your own API)**: `import "…/gamekit"`, register
  handlers, and call `engine.Play(...)` directly. The SDK depends only on **port interfaces**
  (`GameStore`, `SessionStore`, `PrizeStore` with atomic `Deduct`, `WalletStore`, `Cache`, `Locker`,
  `Clock`, `RandSource`, `EventSink`, `TxRunner`). The consumer implements those ports (their own DB,
  auth, transport) — or imports our `adapters` for batteries-included pg/mysql + Redis — and writes
  **their own HTTP/gRPC API**. No dependency on our Core/BFF/proto.
- **Mode B — run the hosted API**: deploy `core` (gRPC) + `bff-consumer`/`bff-admin` (REST). Core is
  simply *one composition*: `gamekit` + the `adapters` + gRPC transport. Nothing in `gamekit` knows
  about gRPC, the `{code,message,trace_id,data}` envelope, snake_case, or HTTP — those live in
  Core/BFF only (reinforcing the Core-is-business / BFF-is-presentation boundary).

Boundaries: `gamekit` (no I/O, no transport) → `adapters` (implement ports over SQL/Redis) → `core`
(wire + gRPC) → BFFs (REST). The SDK is independently versioned and publishable as its own Go
module (semver); breaking a port is an SDK major bump, decoupled from `/api/v{n}`. This makes "some
people only want my logic" a first-class, supported path.

### Tenancy & identity (multi-tenant, multi-merchant; phone/email)

The library is generic and serves **many tenants/merchants**, and the *same real person* (same
phone) can participate in **multiple tenants independently**. Modeled in three layers:

- **Tenancy hierarchy**: `Tenant` (platform account / org) → `Merchant` (brand/store; one tenant
  has N merchants) → `Campaign` → `Game`. **Every** persisted row carries `tenant_id`;
  campaign-scoped rows also carry `merchant_id`. This is the hard **isolation + authorization**
  boundary: an admin JWT is scoped to a tenant (and optionally a merchant); the SDK takes a
  `Scope{tenant_id, merchant_id}` on every call and the adapters filter every query by it.
- **Identity (global person)** — internal `identities` table: a real person identified by
  **verified contacts** — `phone` (normalized E.164) and/or `email` (lowercased), **each globally
  unique** and resolving to one identity. *Either* contact identifies the person; an identity may
  hold both. A newly verified contact that already maps to an identity links to that same person
  (identity merge); otherwise it creates a new identity.
- **Player (tenant-scoped membership)** — `players` row = an identity participating in a tenant,
  `UNIQUE(tenant_id, identity_id)`. Tenant-scoped profile, collected fields, turn balances, wallet,
  and history hang off the **player**. So **same phone → one identity → many players across
  tenants**, each fully isolated — exactly the requirement.

Isolation vs linking: tenants are isolated by default; the global identity is **internal infra**
(platform-level dedup/anti-fraud) and never exposes one tenant's data to another. Auth accepts
`phone` **or** `email` (OTP / magic-link / social / code); verification resolves-or-creates the
identity, upserts the tenant player, and issues a player JWT carrying `tenant_id` / `merchant_id?` /
`player_id` / `identity_id`. **SDK ports** `TenantStore` + `IdentityStore` let embedders plug their
own existing user/identity system while reusing the game logic.

**Wallet/turn scope is configurable** via `wallet_scope` (`campaign` | `merchant` | `tenant`,
**default `campaign`**) set on tenant config and overridable per campaign. Balances are keyed by
`(tenant_id, scope_key, currency)` where `scope_key` = `campaign_id` | `merchant_id` | `tenant_id`
per the setting — so the same schema supports one-off events (per-campaign) and brand loyalty
wallets (per-merchant/tenant) without change.

### Generic engine — the heart
Three core operations the SDK engine exposes (Mode A calls them directly; Mode B exposes via gRPC→REST):
- `Start(gameId, userId)` → creates a **session**, runs the configured **seed generator**,
  returns `{ sessionId, seedData, expiresAt }`.
- `Play(sessionId, payload)` → checks eligibility, runs the **validator** (anti-cheat), runs the
  configured **reward handler**, atomically deducts stock + writes history (+ updates leaderboard
  / inventory), returns `{ rewards[], metadata }`.
- `Eligibility(gameId, userId)` → remaining turns / can-play.

Registry interfaces (string-keyed, pluggable — adding a shape = register one impl):
```go
type RewardHandler interface { Evaluate(ctx, GameConfig, SeedData, Payload) (RewardResult, error) }
type SeedGenerator interface { Generate(ctx, GameConfig, Session) (SeedData, error) }
type Validator     interface { Validate(ctx, GameConfig, Session, Payload) error }
```
Game config (JSON column) declares `seedGenerator`, `rewardHandler`, `validator`, `handlerConfig`,
`rules`, `ui`. Engine core never changes per game.

Reward handlers to ship:
- `probability` — weighted random over prizes (spin/scratch).
- `score_to_tier` — map `payload.score` → tier → weighted random in tier (egg-catcher).
- `collect_items` — verify `payload.caughtItems` against seed drop-plan, aggregate (gift-catcher).
- `lucky_item` — spin awards an intermediate **lucky item** into the player wallet (collection game).

**Reward post-processing (uniform for all handlers)**: a handler returns `rewards[]` where each
reward has a `type`. The engine routes by type inside the `Play` txn — `points` (and `lucky_item`)
are **credited to the player wallet ledger**; `voucher/physical/…` create a **fulfillment task**.
So one play can yield *prize + points together*, and points **accumulate across plays**.

### DB layer (no ORM, dual-engine) — the `adapters/sqlstore` package

This is the **default implementation of the `gamekit` `*Store` ports** (Mode B / batteries-included).
The SDK logic never imports it; consumers may swap their own.
- `database/sql` + `github.com/jmoiron/sqlx`; drivers `pgx/v5/stdlib` (pg) + `go-sql-driver/mysql`.
- `pkg/dialect`: write SQL with `?` placeholders, `db.Rebind()` converts to `$1…` for Postgres;
  helpers for JSON column type (`JSONB` vs `JSON`) and `RETURNING` vs `LastInsertId`.
- **Atomic stock deduction** = the `PrizeStore.Deduct` port impl (works on both engines, no extra
  lock service): `UPDATE prizes SET remaining = remaining - 1 WHERE id = ? AND remaining > 0` →
  check `RowsAffected == 1`; the SDK calls it within a `TxRunner` transaction alongside
  turn-decrement + history insert. Optional `SELECT … FOR UPDATE` for multi-row collect handlers.
- Migrations: split SQL per engine under `adapters/sqlstore/migrations/{postgres,mysql}`, embedded
  via `embed.FS`, run with `pressly/goose` (supports both dialects) on startup or via `make migrate`.

### Caching & fast state — Redis / Dragonfly

A single Redis-protocol store (**Dragonfly** in deploy; any Redis works — same `go-redis` client)
backs all hot/ephemeral state. SQL stays the durable system of record; Redis is cache + ephemeral
state, never the source of truth for money/stock.

- **Ephemeral state** (`adapters/redisstore` — implements the SDK `SessionStore`/`Cache`/`Locker`
  ports; consumers may provide their own):
  - **Sessions**: `Start` writes the session (seed data, `expires_at`) to Redis with native TTL;
    `Play` reads it from there (DB as durable fallback/audit). Natural fit for short-lived data.
  - **Idempotency keys**: `Idempotency-Key → result` stored with TTL so retried `Play`/`claim`
    are safe across instances.
  - **Rate limiting**: distributed token bucket in Redis (per player/IP) so limits hold across
    multiple BFF/Core replicas.
  - **Leaderboard rankings**: Redis **sorted sets** (`ZADD`/`ZREVRANK`/`ZRANGE`) give O(log n)
    real-time rank, around-me, and top-N — updated inside `Play`; DB holds the durable entries and
    `finalize` snapshot. This is what makes the "Đua Top" real-time view cheap.
  - **Stock**: still guarded by the **DB atomic conditional UPDATE** (not Redis) — correctness over
    speed. An optional Redis counter may pre-check "likely out of stock" to shed load, but the DB
    update is authoritative.
- **BFF — read-model cache** (`bffkit/internal/cache`, used by the consumer BFF): cache the assembled widget view models —
  public campaign/game config, display prize list, recent-winners marquee, top-N leaderboard — with
  short TTLs and **explicit invalidation** on the relevant admin mutation (e.g. `UpdateGame` busts
  `public:game:{id}`). Cache-aside pattern; keys namespaced by `tenant_id`.

### Realtime delivery — WebSocket broadcast (recommended default)

Core gameplay (`start`/`play`) stays request/response — a player acts and gets one result, so a
socket adds nothing there. Sockets are used for the **broadcast read surface**: live leaderboard
during a contest, recent-winners marquee, and campaign live counters.

- **Consumer-BFF WebSocket gateway** (`GET /api/v1/ws?campaign_id=…`, JWT/widget-token auth):
  clients subscribe to topics (`leaderboard:{lb_id}`, `winners:{campaign_id}`,
  `counters:{campaign_id}`).
- **Core publishes domain events** (`prize_won`, `play_completed`, `leaderboard_updated`) to
  **Redis/Dragonfly pub/sub**; the BFF gateway fans them out to subscribed sockets. This scales
  horizontally (any BFF replica receives the pub/sub message). Same events feed the Integration hub.
- Graceful fallback: clients that can't hold a socket **poll the cached read endpoints**; an
  optional **SSE** endpoint (`/api/v1/sse`) can serve broadcast-only consumers. Adjustable to
  SSE-only or polling-only per deployment without touching Core (BFF concern).
- Build phase: **Phase 9.5 — Realtime gateway** (WS gateway in `bff-consumer` + Redis pub/sub
  fan-out + topic auth), after BFF hardening.

### Multiplayer & social-interactive games (interaction models)

A game's **`interaction_model`** is a new top dimension (orthogonal to `reward_handler`). The base
plan covers the first; multiplayer adds two more, reusing player/wallet/reward/fulfillment/anti-cheat:

- **`single_session`** (base) — one player, request/response (`start`/`play`). Spin, scratch,
  egg-catcher, gift-catcher, collection.
- **`shared_goal`** (async social / co-op — *MoMo "ủi ủi" / team Lắc Xì fit here*) — players form
  a **Group/Team**, each member's plays/actions **contribute to a shared counter/pool** over time;
  on reaching the goal, a **group reward** is split (equal / by-contribution / top-N). Built on
  existing primitives — new **Group context** (teams, membership, **invite/viral loop**,
  contribution ledger) + a shared-goal counter (Redis live + durable DB) + event bus + reward
  handler + fulfillment outbox + wallet. Realtime is broadcast-only (live progress bar via the
  existing WS gateway). **No re-architecting; engine stays stateless.**
- **`realtime_room`** (synchronous — live head-to-head / live co-op) — a **Match/Room** with
  **server-authoritative live state**, bidirectional WS actions, a server tick loop,
  presence/reconnection, optional matchmaking. Requires a **new stateful `match` service**
  (separate deployable: rooms pinned/sharded per instance, in-memory state + Redis for
  fan-out/persistence) — deliberately separate from the stateless engine. Anti-cheat: clients send
  *intents*, the server resolves and broadcasts deltas (never client-trusted outcomes). On match
  end, results flow into the standard reward/fulfillment/leaderboard pipeline.

Reused across all three: auth/player, wallet, reward handlers, prize stock, fulfillment outbox,
leaderboard, anti-cheat (server-authoritative), observability, Redis pub/sub. New surface is the
Group context (Core) and, for sync only, the `match` service + its WS protocol.

Build phases (both committed — added beyond the base full-feature set):
- **Phase 12 — Group & shared-goal (async multiplayer)**: Group/Team CRUD + invite/join,
  contribution ledger, shared-goal counter, group reward split. Covers MoMo-style team games.
- **Phase 13 — `match` service (realtime sync multiplayer)**: room lifecycle
  (create/join/ready/start/end), authoritative state + tick, bidirectional WS protocol,
  reconnection, results → reward pipeline. Depends on Phase 12's group/reward plumbing.

### Observability — tracing, metrics, logs (Grafana stack)

Wired from Phase 0 so every span/metric/log exists as features land. One **OpenTelemetry**
pipeline; one **Grafana** pane of glass over the **LGTM** backends.

- **Tracing (OTel + Tempo)**: OTel SDK in both services, OTLP export → OpenTelemetry Collector →
  **Tempo**. Auto-instrument the boundaries: gRPC (`otelgrpc` interceptors, BFF↔Core), HTTP
  (`otelhttp` at the BFF edge + n8n callbacks), SQL (`otelsql`, both drivers), Redis
  (`redisotel`). **W3C `traceparent`** propagates BFF→Core→DB/Redis so one trace spans the whole
  `play`. The **`trace_id`** in the response envelope is the OTel trace id — a client error links
  straight to its trace in Grafana.
- **Metrics (Prometheus)**: `/metrics` on each service scraped by **Prometheus**. RED per
  endpoint/RPC (rate, errors, p50/p95/p99 duration) + **business metrics**: plays, wins, prizes
  issued, `prizes.remaining` gauge, fulfillment-task states (pending/failed/dead), dispatcher lag,
  cache hit ratio, rate-limit rejections, WS connections, leaderboard updates.
- **Logs (slog → Loki)**: structured JSON `slog` enriched with `trace_id`/`span_id`/`tenant_id`,
  shipped to **Loki**; Grafana correlates log↔trace by `trace_id` (exemplars link metrics↔traces).
- **Dashboards & alerts**: provisioned Grafana dashboards (per-service RED, gameplay funnel,
  prize/stock health, fulfillment outbox, leaderboard, cache/Redis) + Grafana/Alertmanager rules:
  error-rate & p99 SLO burn, `out-of-stock` spikes, **fulfillment dead-letter growth / dispatcher
  lag**, cheat-flag rate, Redis/DB saturation.
- **Health/readiness**: `/healthz` + `/readyz` (DB + Redis + Core reachability) for orchestrators.

### Anti-cheat mechanism (layered, config-driven)

Defense in depth — each layer is independent; a game's `validator` + `anti_cheat` config selects
how strict to be (`none` | `basic` | `strict`), so it stays config-driven.

1. **Server-authoritative outcomes** — the FE never decides or sends the prize. For `probability`
   and `collect_items`, the *server* picks the result; the client only reports raw inputs (score,
   caught item ids). Tampering the response is meaningless because the server already wrote it.
2. **Server-issued single-use sessions** — `Start` mints a session (`started_at`, `expires_at`,
   bound to `player_id`+`game_id`, stored in Redis with TTL + an opaque secret). `Play` rejects
   unknown/expired/already-consumed sessions and marks the session consumed (one play per session).
3. **Signed payload proof (`strict`)** — `Start` returns a per-session HMAC secret; the FE signs
   its `payload` (and, for action games, a compact replay: `[{t, x}]` catch events). `Play`
   verifies the HMAC and that the replay is internally consistent (event count == score, timestamps
   monotonic, spacing ≥ threshold) — blocks hand-crafted requests from DevTools.
4. **Validator registry** (per game): `basic` (session + rules), `time_and_score_range` (min/max
   play duration, theoretical score ceiling), `drop_plan` (every claimed `drop_id` must exist in
   the server-generated sequence, respect `max_catchable`, pass timing sanity). Adding a check =
   register one validator; the engine core is untouched.
5. **Server-generated seeds** — for `collect_items`, the *server* generates the drop sequence in
   `Start` and stores it; `Play` validates caught ids against it. The client can't invent drops.
6. **Turn + rate enforcement** — eligibility (max plays per user/day, campaign window) checked in
   the `Play` txn; Redis token-bucket rate limits per player/IP; **idempotency keys** stop a
   winning `Play` from being replayed.
7. **Leaderboard anti-cheat** — score ceiling, statistical outlier (z-score) + play-velocity +
   score-consistency checks, and multi-account/device-fingerprint detection → suspicious entries
   move to a `flagged` state for review; admin can `disqualify`/`adjust`. `finalize` only awards
   un-flagged ranks.
8. **Audit & observability** — every `Play`/claim/redeem writes immutable history with the inputs,
   chosen outcome, validator verdict, and `trace_id`; flagged anomalies are queryable for review
   (and feed optional ML anomaly detection later).

### Prize fulfillment & delivery (config-driven, pluggable + n8n)

Delivery and redemption are **configured per prize/campaign, not coded per prize**. Principle:
*our system is the source of truth for what is owed; an orchestrator owns how it is delivered.*

- **Redemption modes**: `instant` (auto on win) · `on_claim` (player calls claim) · `manual`
  (staff approval queue) · `exchange` (lucky-item milestone redeem).
- **Delivery channels** via a `FulfillmentProvider` registry (same pattern as reward handlers,
  keyed by channel — adding a channel = register one provider): built-in `voucher_code` (pop from
  imported code pool), `sms`, `zns`, `email`, `points_credit`, `physical_shipping`, `crm_sync`,
  `ecommerce` (Haravan/Sapo), plus **`external_workflow`** → hand off to **n8n**.
- **Transactional outbox (reliability)**: winning/claiming writes a `fulfillment_tasks` row
  (`status=pending`) in the **same DB txn** as stock deduction + history — no lost/duplicate
  delivery. A **dispatcher** worker polls pending tasks and invokes the configured provider with
  **retry/backoff**, an **idempotency key**, and a **dead-letter** state after N attempts:
  `pending → processing → fulfilled | failed | dead`. Admin can retry/revoke.
- **n8n orchestration (recommended for flexible flows)**: the `external_workflow` provider POSTs
  the task to a per-campaign **n8n webhook** (configurable URL + HMAC secret); n8n runs the visual
  no-code workflow and calls back `POST /fulfillment/tasks/{task_id}/callback` (signed) to mark
  `fulfilled`/`failed` with a `receipt` (voucher code, tracking no., …). Our outbox tracks the
  lifecycle; n8n owns the steps. Alternatives considered: Temporal (durable, dev-heavy),
  Zapier/Make (SaaS), built-in-only (simplest). Chosen: **built-in providers for common channels
  + n8n via `external_workflow`** for custom/long-tail flows.

```jsonc
// prize.fulfillment (config)
{ "redemption_mode": "on_claim",         // instant|on_claim|manual|exchange
  "channel": "external_workflow",         // voucher_code|sms|zns|email|points_credit|physical_shipping|crm_sync|ecommerce|external_workflow
  "channel_config": { "webhook_url": "https://n8n.internal/webhook/prize-deliver",
                      "hmac_secret_ref": "secret://n8n_prize", "collect_fields": ["phone","address"] },
  "retry": { "max_attempts": 5, "backoff": "exponential" },
  "notification": ["zns"] }
```

Tables: `fulfillment_tasks` (the outbox). Reuses `rewards`/`prizes`. Integrates with the existing
Integration Context events (`prize_won`, `prize_claimed`) and Reward `claim`/`fulfill`/`revoke`.

---

## Build phases (vertical slice first, then breadth)

**Phase 0 — Foundation** (runnable skeleton)
- `go.work`, modules, `buf` config, first `.proto` (engine), generate into `pkg/gen`.
- **`gamekit` SDK skeleton**: `types/` + `ports/` interfaces + empty `engine`/`registry` — compiles
  with no DB/transport deps (the publishable library boundary).
- **`adapters`**: `sqlstore` (dual-engine pool, `pkg/dialect`, goose migrations) + `redisstore`
  implementing the ports; verified by a port-contract test suite reusable by external implementers.
- `core/platform`: config loader (env), slog logging, health/ready, **OTel telemetry init**
  (traces+metrics, OTLP export) with gRPC/HTTP/SQL/Redis instrumentation + `trace_id` in the envelope.
  `pkg/apierr`.
- `core` boots a gRPC server; `bff-consumer` + `bff-admin` each boot an HTTP server (sharing
  `bffkit`) with a gRPC client; health + `/metrics` + a sample traced request visible in Tempo.
- `deploy/docker-compose.yml` (pg + mysql + **dragonfly** + **otel-collector + grafana +
  prometheus + tempo + loki**), Makefile targets, goose migration runner.

**Phase 1 — Generic engine MVP (the vertical slice)**
- Tables (in `sqlstore`): `games`, `prizes`, `sessions`, `play_history` — **every row carries
  `tenant_id`** (+ `merchant_id` where campaign-scoped); the `Scope` is threaded through all ports
  and enforced in every query. Sessions in Redis (TTL) with the DB row as durable audit;
  idempotency keys in Redis (namespaced by scope).
- **`gamekit` engine** orchestrator + registries; `probability` handler, `none` seed, `basic`
  validator — all written against ports, unit-testable with in-memory fakes (no DB needed). This is
  the **Mode A deliverable**: the SDK works standalone here.
- `core` wires `gamekit` + `adapters` and exposes gRPC: `StartGame`, `Play`, `CheckEligibility`,
  `GetHistory`; `PrizeStore.Deduct` runs inside the `TxRunner` transaction (Mode B).
- Consumer BFF: `POST /api/v1/games/{id}/start`, `POST /.../play`, `GET /.../history/me`,
  `GET /.../eligibility`. Admin BFF: minimal `POST /api/v1/admin/games` + prizes (to seed the slice).
- **End-to-end spin wheel runs** against both pg and mysql. Unit tests for handler + stock race.

**Phase 2 — More game shapes**
- `score_to_tier` + `time_and_score_range` (egg-catcher); `collect_items` + `drop_sequence` seed
  + `drop_plan` validator (gift-catcher). History stores `rewards` JSON. Tests per handler.

**Phase 3 — Reward system**
- Prize CRUD, constraints (max_per_user/max_per_day), code import, `claim`/`fulfill`/`revoke`,
  fulfillment metadata. Admin + player-facing reward queries.

**Phase 3.5 — Fulfillment & delivery (outbox + providers + n8n)**
- `fulfillment_tasks` outbox written in the win/claim txn; dispatcher worker with retry/backoff +
  dead-letter; `FulfillmentProvider` registry (built-in `voucher_code` first, others stubbed);
  `external_workflow` provider → n8n webhook + signed `…/callback` endpoint; redemption modes
  (instant/on_claim/manual/exchange); admin task list + retry.

**Phase 4 — Tenancy, identity & player system**
- `tenants`, `merchants`, `identities` (global, unique phone/email contacts), `players`
  (`UNIQUE(tenant_id, identity_id)`) tables; Tenant/Merchant CRUD (platform/tenant admin).
- **Identity by phone/email**: resolve-or-create on verify, contact linking/merge; auth methods
  (code/OTP/magic-link/social as pluggable providers — OTP/social as interfaces with dev stubs).
- Tenant-scoped player profile, turn balance per campaign, cross-game history; JWT carries
  `tenant_id`/`merchant_id`/`player_id`/`identity_id`. Verify the same phone yields one
  `identity_id` across tenants but isolated `player_id`s.

**Phase 5 — Campaign**
- Campaign aggregate (under a merchant; settings, channels, auth methods, links games/quests),
  CRUD, duplicate,
  analytics summary. Public campaign config endpoint for the FE widget.

**Phase 6 — Quest/Mission**
- Quest handler registry (daily_checkin, share_social, invite_friend, scan_qr, view_page,
  answer_question, external_event), `complete` → grant `play_turn`. Eligibility reads turns.

**Phase 7 — Leaderboard**
- Config (metric types, time windows fixed/recurring/manual, prize tiers), entry update hook
  fired from `Play` → **Redis sorted set** for real-time ranking / around-me / my-rank (DB holds
  durable entries), `finalize` (lock + snapshot + batch award), anti-cheat flags + disqualify/adjust.

**Phase 8 — Wallet, Points & Exchange**
- `lucky_item` handler + `points` reward type → **wallet ledger** credit inside `Play`, keyed by
  the configurable `wallet_scope` (campaign/merchant/tenant); milestones config with both modes
  (`cumulative_unlock` auto-grant/claim, `spend_exchange`); `redeem` (atomic threshold check →
  spend/grant-once → fulfillment task); wallet balance + ledger queries.

**Phase 9 — BFF hardening (both services)**
- Consumer BFF: player/public JWT + widget-token auth, **Redis distributed rate limiting** on
  `/play`/`/start`, **read-model cache** (cache-aside, TTL + invalidation) for widget config +
  winners marquee + top-N leaderboard, embed CORS.
- Admin BFF: role-based authz (admin/designer/reward_manager), admin aggregation endpoints, signed
  n8n fulfillment callback, no public exposure. Both built on shared `bffkit`.

**Phase 10 — Integration hub**
- Internal event bus (Redis pub/sub); adapters as interfaces with stub impls: webhook, Google
  Sheet, SMS/ZNS, CRM, and **n8n** (the `external_workflow` fulfillment provider lives here).
  Triggered on `play_completed`, `prize_won`, `prize_claimed`, `quest_completed`,
  `leaderboard_finalized`.

**Phase 11 — Cross-cutting polish**
- OpenAPI for BFF REST, generated gRPC docs, seed/demo data, integration tests across pg+mysql in
  docker-compose, READMEs, error-code reference.
- **Observability finishing**: business metrics instrumentation across all contexts, provisioned
  **Grafana dashboards** (RED, gameplay funnel, prize/stock, fulfillment outbox, leaderboard,
  cache) + **alert rules** (SLO burn, out-of-stock, dead-letter growth, cheat-flag rate).

---

## Key files to create (representative)
- **SDK**: `gamekit/ports/ports.go` (port interfaces), `gamekit/engine/engine.go` (Start/Play),
  `gamekit/registry.go`, `gamekit/handlers/probability.go`, `gamekit/reward/stock_policy.go`
- **Adapters**: `adapters/sqlstore/prize.go` (atomic `Deduct`), `adapters/sqlstore/db.go`
  (dual-engine pool), `adapters/redisstore/session.go`, `adapters/redisstore/leaderboard_zset.go`,
  `adapters/sqlstore/migrations/{postgres,mysql}/0001_init.sql`, `pkg/dialect/dialect.go`
- **Port-contract tests**: `gamekit/ports/contract/` (shared suite any adapter runs)
- **API/transport**: `proto/game/v1/engine.proto` (+ others), `core/internal/grpcsvc/engine.go`,
  `core/cmd/core/main.go` (wires gamekit+adapters), `core/platform/telemetry.go`
- **BFF**: `bffkit/internal/envelope/envelope.go`, `bffkit/internal/cache/cache.go`,
  `bff-consumer/internal/game/handler.go`, `bff-admin/internal/fulfillment/callback.go`
- `deploy/docker-compose.yml`, `Makefile`, `README.md` (+ an SDK "embed" example under `examples/`)

## Dependencies
`google.golang.org/grpc`, `bufbuild` (buf for codegen), `github.com/jmoiron/sqlx`,
`github.com/jackc/pgx/v5`, `github.com/go-sql-driver/mysql`, `github.com/pressly/goose/v3`,
`github.com/redis/go-redis/v9` (Redis/Dragonfly client — sessions, cache, rate-limit, leaderboard
ZSET; instrumented via `redisotel`), a router for BFF (`chi`), `golang-jwt`, `slog`. (Distributed
rate limiting uses Redis; `golang.org/x/time/rate` only as an in-process fallback.)
Observability: `go.opentelemetry.io/otel` (+ OTLP exporter), `otelgrpc`, `otelhttp`,
`github.com/XSAM/otelsql`, `redisotel`, `prometheus/client_golang`.

## Verification
- `make generate` (buf), `make up` (docker-compose: pg+mysql+dragonfly), `make migrate
  DRIVER=postgres` and `=mysql`.
- **Mode A (SDK embed)**: `go test ./gamekit/...` runs the engine with in-memory fake ports (no DB)
  — proves the logic is usable standalone. `examples/embed` is a ~40-line program that imports
  `gamekit` + `adapters` and runs a `Play` with **no Core/BFF**, demonstrating "use my logic, own API".
- **Mode B (hosted API)**: run `core` + `bff-consumer`; `curl` the spin-wheel flow end-to-end:
  1. create a game (admin BFF) with `probability` handler + prizes,
  2. `POST /api/v1/games/:id/start` → `session_id`,
  3. `POST /api/v1/games/:id/play` → a reward, with stock decremented,
  4. `GET /api/v1/games/:id/history/me` shows the play.
- `go test ./...`: unit tests for each handler/validator; the **port-contract suite** run against
  both `sqlstore` (pg+mysql) and `redisstore`; a **concurrency test** firing N parallel `Play`
  calls against a limited-stock prize asserting no over-issue (both engines). Repeat for
  egg-catcher (score) and gift-catcher (collect) in Phase 2.
- **Observability check**: open **Grafana** (`http://localhost:3000`), confirm the spin-wheel
  `play` shows as **one end-to-end trace** (BFF → Core gRPC → SQL/Redis) in Tempo, the RED + prize
  metrics render on the dashboard, and a forced error's `trace_id` (from the response envelope)
  finds its trace + correlated Loki logs.

## Notes / non-goals for first cut
- OTP/social/CRM/SMS/ZNS providers are wired as **interfaces with dev stubs** — real provider
  integrations are out of scope for the initial build but pluggable.
- **Redis/Dragonfly** is part of the stack from the start (sessions, idempotency, rate limiting,
  leaderboard sorted sets, BFF read-model cache). It is **not** the source of truth for
  stock/rewards — those stay in SQL with atomic conditional updates; Redis is cache + ephemeral
  state. Dragonfly is the default deploy image (Redis-compatible, multi-threaded); swappable for
  Redis/Valkey via the same client + connection string.

---

# API Design

Layers: **two BFFs** (REST/JSON) over a shared **Core gRPC** (internal). The REST surface is the
public contract; each REST handler maps to one or more Core RPCs.

- **Consumer BFF** (`bff-consumer`) — public-internet facing, serves the widget + player: the
  generic game engine endpoints, players/auth/wallet/turns, quests, public leaderboard reads,
  milestones, `/public/*`, and the realtime `/ws` + `/sse`. Aggressive caching + per-player/IP
  rate limits + CORS for embed.
- **Admin BFF** (`bff-admin`) — internal/VPN facing, serves the dashboard + machine callbacks: all
  `/admin/*` management endpoints and the signed n8n `/fulfillment/tasks/{id}/callback`. Stricter
  authz (admin/designer/reward_manager roles), no public exposure, no embed CORS.

Both import `bffkit` (coreclient, envelope/error mapping, middleware, cache) so behavior stays
consistent. Splitting by audience lets each scale and be secured independently (the admin surface
never sits on the public edge). Each BFF ships its own OpenAPI 3.1 spec
(`bff-consumer/api/openapi.yaml`, `bff-admin/api/openapi.yaml`), both linted with
`npx @redocly/cli lint`.

**Versioning is mandatory on every route**: all REST paths are prefixed `/api/v{n}` (currently
`/api/v1`) — no unversioned endpoints, including `/api/v1/ws`, `/api/v1/sse`, health stays at
`/healthz`/`/readyz` (ops, unversioned). Core gRPC is versioned by package (`game.v1`). A breaking
change bumps the path to `/api/v2` and the proto package to `game.v2`, run in parallel ≥90 days.

### Responsibility boundary — Core = business, BFF = presentation

**Core owns business objects and rules only — it is UI-agnostic.** Core's gRPC contracts expose
canonical domain entities (`Game`, `Prize`, `PlayResult`, `Session`, `Player`, `Campaign`,
`Quest`, `Leaderboard`, `InventoryItem`) with **business semantics**: eligibility, weighted
random, stock integrity, anti-cheat validation, turn balances, rankings, fulfillment state. Core
does **not** know about the response envelope, snake_case JSON, HTTP status, pagination cursors,
localization, marquees, or any rendering concern. It returns whole domain objects (or business
errors via `google.rpc.Status` details); it never tailors output to a screen.

**BFF owns everything UI-facing — it enriches and converts.** Given a UI need, the BFF:
- **aggregates** multiple Core RPCs into one view model (e.g. widget render = `GetPublicConfig`
  + `ListPrizes` + recent winners in a single `GET /public/campaigns/{id}` response);
- **shapes & redacts** for the audience (public prize list strips `probability`/`stock`; admin
  sees them) — Core returns the full object, BFF decides what each caller may see;
- **applies the envelope** (`code`/`message`/`trace_id`/`data`), snake_case field naming,
  cursor pagination, HTTP status, localization, and field selection;
- **caches** hot/derived read models (config, winners marquee) without Core involvement.

Consequence for design: the **`ui` block** (theme, cover image, custom assets) is admin-authored
*business configuration* — Core stores it as an **opaque JSON blob** (persists/returns it, never
interprets it); the BFF/widget is the only layer that reads its contents. So a new UI requirement
is a BFF/widget change, not a Core change — reinforcing the config-driven goal. Core RPC field
names are the proto's own convention; the snake_case JSON contract is purely a BFF concern.

## Conventions

- **Base path / versioning**: `/api/v1`. Version in the URL path. Breaking changes → `/api/v2`;
  old version deprecated with `Deprecation` + `Sunset` headers (min 90 days).
- **Naming**: **`snake_case`** for all JSON fields and query params, everywhere. Resource URIs are
  nouns, plural collections, no verbs (engine actions like `start`/`play` are sub-resource action
  endpoints, acceptable for non-CRUD operations).
- **IDs**: prefixed opaque strings — `game_…`, `prize_…`, `sess_…`, `play_…`, `camp_…`,
  `quest_…`, `lb_…`, `item_…`, `player_…`, `tenant_…`.
- **Uniform response envelope** — *every* response, success or error, has the same top-level shape:
  ```jsonc
  { "code": 0,            // int — canonical status; 0 = OK, non-zero = error (google.rpc.Code)
    "message": "ok",      // human-readable, actionable
    "trace_id": "01H... ",// request/trace correlation id; also echoed in the X-Trace-Id header
    "data": { /* … */ } } // payload on success; null on error
  ```
  `trace_id` is generated per request (propagated via OpenTelemetry) so a client error always maps
  to a server log line. HTTP status is still set per Google's code↔HTTP mapping (below).
- **Collections**: `data` holds `{ "items": [...], "pagination": { "next_cursor": string|null,
  "has_more": bool, "total": int? } }`. Cursor pagination (`?cursor=&limit=`, default 20, max 100);
  filters as query params (`?status=active&type=spin_wheel`).
- **Single resource**: `data` holds the resource object.
- **Auth levels** (Bearer JWT unless noted):
  - **Public** — no auth or a short-lived widget token (`X-Widget-Token`); read-only render data
    + gameplay for an authenticated player session.
  - **Player** — player JWT (issued by `/players/auth/*`); gameplay + own data.
  - **Admin** — admin/staff JWT with roles (`admin`, `designer`, `reward_manager`); management.
- **Multi-tenancy**: every entity carries `tenant_id` (campaign-scoped entities also `merchant_id`);
  resolved from the JWT (admin) or campaign/widget token (public), **not** the URL path. All
  adapters filter every query by the `Scope{tenant_id, merchant_id}`. Players are tenant-scoped and
  back a **global identity** (phone/email) — see Tenancy & identity.
- **Idempotency**: mutating gameplay/claim endpoints accept an `Idempotency-Key` header; Core
  stores the key→result for the session TTL to make retries safe.
- **Rate limiting**: per-player + per-IP token bucket on `/play`, `/start`, auth endpoints. `429`
  returns `Retry-After`. Limits configurable per campaign.

### Error model (Google API best practice)

Errors keep the same envelope. `code` is the canonical `google.rpc.Code` integer, `message` is
human-readable, and `data.error` carries structured `ErrorInfo` for machine handling — a **stable**
`reason` (UPPER_SNAKE_CASE, never renamed), the owning `domain`, and `metadata`. Field-level
validation adds `field_violations[]` (mirrors `google.rpc.BadRequest`). gRPC `status.details`
carry the same payload internally and the BFF transcodes them.

```jsonc
{ "code": 9,                                  // FAILED_PRECONDITION
  "message": "All units of 'Voucher 100K' have been awarded.",
  "trace_id": "01HX3...",
  "data": {
    "error": {
      "status": "FAILED_PRECONDITION",        // canonical code name
      "reason": "PRIZE_OUT_OF_STOCK",         // stable machine-readable reason
      "domain": "muse.game",
      "metadata": { "prize_id": "prize_001", "game_id": "game_001" }
    }
  } }
```

Field-validation example (`code: 3` INVALID_ARGUMENT):
```jsonc
"data": { "error": { "status": "INVALID_ARGUMENT", "reason": "VALIDATION_FAILED",
  "domain": "muse.game",
  "field_violations": [ { "field": "rules.max_plays_per_user", "description": "must be >= 1" } ] } }
```

### Standard response shapes (referenced below)

> In every endpoint example below, the JSON shown is the **`data`** payload — assume it is wrapped
> in `{ code, message, trace_id, data }`.

```jsonc
// reward_result — the data payload returned by every Play, regardless of game shape
{
  "rewards": [
    { "prize_id": "prize_001", "name": "Voucher 100K", "image": "https://…",
      "type": "voucher", "quantity": 1, "value": 100000 }
  ],
  "metadata": { /* shape-specific: slot_index | score+tier | total_caught… */ }
}
```

---

## 1. Core gRPC contracts (`proto/game/v1`)

Internal service boundary. BFF is the only client. Messages mirror the REST DTOs; errors map to
gRPC `status` codes (and back to RFC 7807 at the BFF).

```protobuf
// Every request carries a Scope; the service enforces tenant/merchant isolation.
message Scope { string tenant_id = 1; string merchant_id = 2; }

service TenantService {                                                // platform-admin
  rpc CreateTenant(...); rpc GetTenant(...); rpc UpdateTenant(...); rpc ListTenants(...);
}
service MerchantService {                                             // tenant-admin (scoped)
  rpc CreateMerchant(...); rpc GetMerchant(...); rpc UpdateMerchant(...); rpc ListMerchants(...);
}
service IdentityService {                                             // global person (internal)
  rpc ResolveOrCreate(...);    // by verified phone/email → identity_id (merge if contact exists)
  rpc LinkContact(...); rpc GetIdentity(...);
}
service EngineService {
  rpc StartGame(StartGameRequest) returns (StartGameResponse);        // create session + seed
  rpc Play(PlayRequest) returns (PlayResponse);                       // validate + reward + persist
  rpc CheckEligibility(EligibilityRequest) returns (EligibilityResponse);
  rpc GetHistory(HistoryRequest) returns (HistoryResponse);
}
service GameAdminService {                                            // game config CRUD
  rpc CreateGame(...) ; rpc GetGame(...) ; rpc UpdateGame(...) ; rpc DeleteGame(...) ; rpc ListGames(...);
}
service RewardService {
  rpc CreatePrize(...); rpc UpdatePrize(...); rpc DeletePrize(...); rpc ListPrizes(...);
  rpc ImportCodes(...); rpc ClaimPrize(...); rpc FulfillPrize(...); rpc RevokePrize(...);
  rpc GetPrizeSummary(...);
}
service PlayerService {
  rpc StartAuth(...); rpc VerifyAuth(...);            // phone/email OTP/magic-link/social/code
  rpc GetProfile(...); rpc UpdateProfile(...); rpc AddContact(...);   // resolve identity + upsert player
  rpc GetTurnBalance(...); rpc GrantTurns(...); rpc ConsumeTurn(...);
}
service CampaignService {
  rpc CreateCampaign(...); rpc GetCampaign(...); rpc UpdateCampaign(...); rpc ListCampaigns(...);
  rpc DuplicateCampaign(...); rpc GetCampaignAnalytics(...); rpc GetPublicConfig(...);
}
service QuestService {
  rpc CreateQuest(...); rpc ListQuests(...); rpc UpdateQuest(...); rpc DeleteQuest(...);
  rpc CompleteQuest(...);                              // verify via handler → grant turns
}
service LeaderboardService {
  rpc CreateLeaderboard(...); rpc UpdateLeaderboard(...); rpc ListLeaderboards(...);
  rpc GetRankings(...); rpc GetAroundMe(...); rpc GetMyRank(...);
  rpc Finalize(...); rpc Reset(...); rpc FlagEntry(...); rpc DisqualifyEntry(...); rpc AdjustScore(...);
}
service InventoryService {
  rpc GetInventory(...); rpc ListExchangeMilestones(...); rpc Redeem(...);   // atomic decrement+award
}
service IntegrationService {
  rpc CreateIntegration(...); rpc ListIntegrations(...); rpc DeleteIntegration(...);
  rpc EmitEvent(...);                                  // internal event bus entry
}
```

Engine messages (the generic part):

```protobuf
message PlayRequest {
  Scope scope = 1;                      // tenant_id + merchant_id (isolation)
  string game_id = 2; string session_id = 3; string player_id = 4;
  google.protobuf.Struct payload = 5;   // shape-specific: {} | {score} | {caught_items}
  string idempotency_key = 6;
}
message PlayResponse { repeated Reward rewards = 1; google.protobuf.Struct metadata = 2; }
```

---

## 2. BFF REST — Game Engine (generic, all game types)

The three generic endpoints every game uses. `payload` is opaque to the engine — routed to the
configured handler.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/games/{gameId}/start` | Public/Player | Create play session; returns seed for FE render |
| POST | `/api/v1/games/{gameId}/play` | Public/Player | Submit payload; returns rewards |
| GET | `/api/v1/games/{gameId}/eligibility` | Public/Player | Remaining turns / can-play |
| GET | `/api/v1/games/{gameId}/history/me` | Player | Caller's play history (paginated) |

```jsonc
// POST /games/{gameId}/start  →  request body
{ "user_id": "player_123" }
// data →
{ "session_id": "sess_001", "seed_data": null,          // null | {duration,max_score} | {drop_sequence:[…]}
  "expires_at": "2026-04-02T10:05:00Z" }

// POST /games/{gameId}/play  →  request  (payload differs per shape, envelope identical)
{ "session_id": "sess_001", "payload": {} }                       // spin/scratch (probability)
{ "session_id": "sess_002", "payload": { "score": 75 } }          // egg-catcher (score_to_tier)
{ "session_id": "sess_003", "payload": { "caught_items": ["d_001","d_003"] } } // gift (collect_items)
// data →  always reward_result (see Standard response shapes)
{ "rewards": [ { "prize_id":"p_001","name":"Voucher 100K","image":"…","quantity":1 } ],
  "metadata": { "slot_index": 3 } }

// GET /games/{gameId}/eligibility  →  data
{ "can_play": true, "remaining_plays": 2, "reason": null,         // reason: "OUT_OF_TURNS"|"NOT_STARTED"|"ENDED"
  "next_reset_at": "2026-04-03T00:00:00Z" }
```

## 3. BFF REST — Game Config (Admin)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/admin/games` | Admin | Create game (template + handler config) |
| GET | `/api/v1/admin/games/{gameId}` | Admin | Get full config |
| PUT | `/api/v1/admin/games/{gameId}` | Admin | Update config |
| DELETE | `/api/v1/admin/games/{gameId}` | Admin | Delete game |
| GET | `/api/v1/admin/games` | Admin | List (filter `campaignId,type,status`, paginated) |
| POST | `/api/v1/admin/games/{gameId}/assets` | Admin | Upload asset (multipart or presigned) |
| GET | `/api/v1/admin/games/{gameId}/assets` | Admin | List assets |
| DELETE | `/api/v1/admin/games/{gameId}/assets/{assetId}` | Admin | Delete asset |

```jsonc
// POST /admin/games  →  request (config-driven; engine reads seed_generator/reward_handler/validator)
{
  "name": "Vòng Quay May Mắn", "type": "spin_wheel", "campaign_id": "camp_001",
  "seed_generator": "none", "reward_handler": "probability", "validator": "basic",
  "ui": { "cover_image": "https://…", "theme": { "primary_color": "#FF5733" },
          "custom_assets": { "wheel": "…", "pointer": "…" } },   // opaque to Core; BFF passes through
  "rules": { "max_plays_per_user": 3, "max_plays_per_day": 1, "require_login": true,
             "start_date": "2026-04-01T00:00:00Z", "end_date": "2026-04-30T23:59:59Z" },
  "handler_config": {                                 // shape-specific, validated per handler
    "prizes": [ { "prize_id": "p_001", "probability": 0.05 },
                { "prize_id": "p_000", "probability": 0.95 } ] },
  "status": "draft"                                   // draft|active|paused|ended
}
// handler_config variants:
//  score_to_tier: { "tiers": [ {"min":0,"max":29,"prize_group":"t0"}, … ],
//                   "prize_groups": { "t1": [ {"prize_id":"p","probability":0.7} ] } }
//  collect_items: { "drops": [ {"type":"voucher_50k","prize_id":"p","frequency":3,"max_catchable":2} ],
//                   "total_items": 40 }
//  lucky_item:    { "items": [ {"item_id":"lucky_star","probability":0.2} ] }   // → inventory
```

## 4. BFF REST — Reward System

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/admin/prizes` | Admin | Create prize |
| GET | `/api/v1/admin/prizes` | Admin | List prizes (filter, paginated) |
| GET | `/api/v1/admin/prizes/{prizeId}` | Admin | Get prize |
| PUT | `/api/v1/admin/prizes/{prizeId}` | Admin | Update prize |
| DELETE | `/api/v1/admin/prizes/{prizeId}` | Admin | Delete prize |
| POST | `/api/v1/admin/prizes/{prizeId}/codes` | Admin | Import codes (bulk) |
| GET | `/api/v1/admin/prizes/summary` | Admin | Stock summary across prizes |
| POST | `/api/v1/players/me/rewards/{rewardId}/claim` | Player | Claim a won prize |
| POST | `/api/v1/admin/rewards/{rewardId}/fulfill` | Admin (reward_manager) | Mark fulfilled |
| POST | `/api/v1/admin/rewards/{rewardId}/revoke` | Admin (reward_manager) | Revoke a reward |
| POST | `/api/v1/fulfillment/tasks/{taskId}/callback` | Service (HMAC) | n8n/orchestrator reports delivery result |
| GET | `/api/v1/admin/fulfillment/tasks` | Admin | List outbox tasks (filter status/campaign/prize) |
| POST | `/api/v1/admin/fulfillment/tasks/{taskId}/retry` | Admin | Re-dispatch a failed/dead task |

```jsonc
// POST /admin/prizes
{ "name": "Voucher 100K", "type": "voucher", "image": "https://…",
  "stock": { "total": 100 }, "value": 100000,
  "constraints": { "max_per_user": 1, "max_per_day": null },
  "fulfillment": { "method": "code", "notification": ["sms","zns"] },
  "metadata": { "voucher_code": "GAME100K", "expiry_date": "2026-05-30" } }
```

## 5. BFF REST — Player System (identity by phone/email)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/players/auth/start` | Public | Begin auth by **phone or email** (OTP/magic-link/social/code) |
| POST | `/api/v1/players/auth/verify` | Public | Verify → resolve identity, upsert tenant player → JWT |
| GET | `/api/v1/players/me` | Player | Profile + verified contacts |
| PUT | `/api/v1/players/me` | Player | Update profile / collected fields |
| POST | `/api/v1/players/me/contacts` | Player | Add+verify a second contact (link to identity) |
| GET | `/api/v1/players/me/turns` | Player | Turn balance (filter `campaign_id`) |
| GET | `/api/v1/players/me/history` | Player | Cross-game play history (within tenant) |

```jsonc
// POST /players/auth/start — phone OR email; tenant/merchant come from the widget token
{ "identifier": { "type": "phone", "value": "+84901234567" },   // type: phone | email
  "method": "otp", "campaign_id": "camp_001" }                  // otp | magic_link | social | code
//   data ← { "challenge_id": "chl_…", "expires_at": "…" }
// POST /players/auth/verify → { "challenge_id": "chl_…", "code": "123456" }
//   data ← { "token": "<jwt>",        // claims: tenant_id, merchant_id?, player_id, identity_id
//            "player": { "player_id":"player_…", "identity_id":"idn_…",
//                        "contacts":[{"type":"phone","value":"+84901234567","verified":true}],
//                        "profile":{…} } }
// Same phone in another tenant → same identity_id, DIFFERENT player_id (isolated).
```

## 5b. BFF REST — Tenant & Merchant (admin)

Platform-admin manages tenants; tenant-admin manages merchants under its own tenant. `tenant_id`/
`merchant_id` are **never** in the path — they come from the JWT scope.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/admin/tenants` | Platform admin | Create tenant |
| GET | `/api/v1/admin/tenants` | Platform admin | List tenants |
| PUT | `/api/v1/admin/tenants/{tenantId}` | Platform admin | Update tenant/plan/settings |
| POST | `/api/v1/admin/merchants` | Admin (tenant) | Create merchant under caller's tenant |
| GET | `/api/v1/admin/merchants` | Admin (tenant) | List merchants (tenant-scoped) |
| PUT | `/api/v1/admin/merchants/{merchantId}` | Admin (tenant) | Update merchant |

```jsonc
// POST /admin/tenants  (platform admin)
{ "name":"Acme Group", "plan":"pro",
  "settings": { "identity_linking": true, "default_locale":"vi", "wallet_scope":"tenant" } }
// POST /admin/merchants  (tenant admin; tenant_id from JWT)
{ "name":"Acme Coffee", "logo":"https://…", "settings": { "wallet_scope_override": null } }
```

## 6. BFF REST — Campaign

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/admin/campaigns` | Admin | Create campaign |
| GET | `/api/v1/admin/campaigns` | Admin | List campaigns |
| GET | `/api/v1/admin/campaigns/{campaignId}` | Admin | Get campaign |
| PUT | `/api/v1/admin/campaigns/{campaignId}` | Admin | Update |
| POST | `/api/v1/admin/campaigns/{campaignId}/duplicate` | Admin | Clone campaign + children |
| GET | `/api/v1/admin/campaigns/{campaignId}/analytics` | Admin | Plays/wins/conversion stats |

```jsonc
// POST /admin/campaigns
{ "name": "Tết 2027", "start_date": "2027-01-01", "end_date": "2027-02-15",
  "channels": ["website_embed","qr_code","chatbot"], "games": ["game_001"],
  "quests": ["quest_001"], "settings": { "require_auth": true,
  "auth_methods": ["phone_otp","zalo"], "collect_fields": ["name","phone"],
  "max_plays_per_user": 5, "max_plays_per_day": 2 } }
```

## 7. BFF REST — Quest / Mission

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/admin/quests` | Admin | Create quest |
| GET | `/api/v1/admin/quests` | Admin | List (filter `campaignId`) |
| PUT | `/api/v1/admin/quests/{questId}` | Admin | Update |
| DELETE | `/api/v1/admin/quests/{questId}` | Admin | Delete |
| GET | `/api/v1/quests` | Player | List available quests + completion state |
| POST | `/api/v1/quests/{questId}/complete` | Player | Verify + grant turns |

```jsonc
// quest types: daily_checkin | share_social | invite_friend | scan_qr | view_page |
//              answer_question | external_event
// POST /admin/quests
{ "campaign_id": "camp_001", "type": "share_social",
  "reward": { "type": "play_turn", "quantity": 2 },
  "config": { "platforms": ["facebook","zalo"] } }
// POST /quests/{questId}/complete → { "proof": { "platform": "facebook", "share_id": "…" } }
//                       data ←       { "completed": true, "turns_granted": 2, "turn_balance": 3 }
```

## 8. BFF REST — Leaderboard

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/admin/leaderboards` | Admin | Create leaderboard config |
| PUT | `/api/v1/admin/leaderboards/{lbId}` | Admin | Update |
| GET | `/api/v1/admin/leaderboards` | Admin | List (filter `campaignId`) |
| POST | `/api/v1/admin/leaderboards/{lbId}/finalize` | Admin | Lock ranking + batch award tiers |
| POST | `/api/v1/admin/leaderboards/{lbId}/reset` | Admin | Reset entries (new window) |
| POST | `/api/v1/admin/leaderboards/{lbId}/entries/{playerId}/disqualify` | Admin | Disqualify |
| POST | `/api/v1/admin/leaderboards/{lbId}/entries/{playerId}/adjust` | Admin | Adjust score |
| GET | `/api/v1/leaderboards/{lbId}/rankings` | Public | Top-N rankings (paginated) |
| GET | `/api/v1/leaderboards/{lbId}/around-me` | Player | Entries around caller |
| GET | `/api/v1/leaderboards/{lbId}/my-rank` | Player | Caller rank + distance to next tier |

```jsonc
// POST /admin/leaderboards
{ "campaign_id": "camp_001", "name": "Top Tuần",
  "metric": "high_score",                       // high_score|total_score|total_wins|total_plays|quest_points|collection_count
  "time_window": { "type": "recurring", "period": "weekly" },   // fixed|recurring|manual
  "prize_tiers": [ { "from_rank": 1, "to_rank": 1, "prize_id": "p_iphone" },
                   { "from_rank": 2, "to_rank": 5, "prize_id": "p_voucher500" } ],
  "anti_cheat": { "score_ceiling": 150, "flag_outliers": true } }
// Updated automatically inside Play; no separate submit endpoint. Play response includes new rank.
```

## 9. BFF REST — Wallet, Points & Exchange (collection / points-accumulation games)

A **wallet** holds named balances (`points`, `lucky_star`, `coin`…) per player, scoped by the
configurable `wallet_scope` (`campaign` default | `merchant` | `tenant`), backed by an append-only
ledger. Plays credit the wallet; milestones convert balances → prizes. Queries filter by the active
scope; `?campaign_id=` narrows when scope is per-campaign.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/v1/players/me/wallet` | Player | All balances (filter `campaign_id`) |
| GET | `/api/v1/players/me/wallet/ledger` | Player | Earn/spend history (paginated) |
| GET | `/api/v1/games/{gameId}/milestones` | Public/Player | Milestone config + caller progress |
| POST | `/api/v1/games/{gameId}/milestones/{milestoneId}/redeem` | Player | Redeem/claim a milestone |

```jsonc
// GET /players/me/wallet → data { "balances": { "points": 750, "lucky_star": 7 } }

// milestones config (in game config) — two modes:
{ "currency": "points",
  "mode": "cumulative_unlock",                 // cumulative_unlock | spend_exchange
  "auto_grant": true,                          // grant the moment threshold is crossed (else player claims)
  "milestones": [ { "milestone_id":"m1","threshold":500,"prize_id":"p_voucher50" },
                  { "milestone_id":"m2","threshold":1000,"prize_id":"p_voucher200" } ] }
//  cumulative_unlock: reach accumulated `threshold` → grant once, points NOT spent (loyalty/tier).
//  spend_exchange:    redeem deducts `threshold` from the balance (shop). Same endpoint.

// GET /games/{gameId}/milestones → data
{ "currency": "points", "balance": 750, "mode": "cumulative_unlock",
  "milestones": [ { "milestone_id":"m1","threshold":500,"prize_id":"p_voucher50",
                    "status":"granted" },                         // reached & awarded
                  { "milestone_id":"m2","threshold":1000,"prize_id":"p_voucher200",
                    "status":"locked","progress":750,"remaining":250 } ] }

// POST /games/{gameId}/milestones/{milestoneId}/redeem  (manual claim or spend)
//   data ← { "redeemed": true, "mode":"spend_exchange", "spent": {"points":500},
//            "reward": { "prize_id":"p_voucher50",… }, "balance": { "points": 250 } }
//   atomic txn: verify threshold/balance → (spend mode) deduct → grant-once (idempotent) → fulfillment task
```

Note: for `cumulative_unlock` + `auto_grant`, the milestone prize is created automatically inside
`Play` when the credit crosses the threshold (idempotent — granted once); the redeem endpoint is
only needed for manual-claim or spend-exchange modes.

## 10. BFF REST — Public / Embed (FE widget, cached)

Lightweight, heavily cached (CDN/Redis later), no admin auth.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/v1/public/campaigns/{campaignId}` | Public | Full render config for the widget |
| GET | `/api/v1/public/games/{gameId}` | Public | Game render config (theme/assets/rules) |
| GET | `/api/v1/public/games/{gameId}/prizes` | Public | Display prizes (no probabilities/stock) |
| GET | `/api/v1/public/games/{gameId}/winners` | Public | Recent winners (marquee) |

## 11. BFF REST — Integration (Admin)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/admin/integrations` | Admin | Register integration (webhook/gsheet/sms/zns/crm) |
| GET | `/api/v1/admin/integrations` | Admin | List (filter `campaignId`) |
| DELETE | `/api/v1/admin/integrations/{integrationId}` | Admin | Remove |

Events emitted to integrations: `play_completed`, `prize_won`, `prize_claimed`,
`quest_completed`, `leaderboard_finalized`, `goal_reached`, `match_ended`. Adapters are interfaces
with dev stubs.

## 12. BFF REST — Group / Team & Shared Goal (async multiplayer)

Players form teams and contribute to a shared goal; the group reward splits on completion.
Consumer BFF for players; admin BFF configures the goal.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/groups` | Player | Create a team (for a campaign/game) |
| GET | `/api/v1/groups/me` | Player | My teams |
| GET | `/api/v1/groups/{groupId}` | Player | Team detail + members + shared-goal progress |
| POST | `/api/v1/groups/{groupId}/invite` | Player | Generate invite code/link (viral loop) |
| POST | `/api/v1/groups/join` | Player | Join via invite code |
| GET | `/api/v1/groups/{groupId}/contributions` | Player | Per-member contribution (intra-team board) |
| POST | `/api/v1/admin/shared-goals` | Admin | Configure shared goal + reward-split policy |
| GET | `/api/v1/admin/groups` | Admin | List teams (filter `campaign_id`) |

```jsonc
// POST /admin/shared-goals
{ "campaign_id":"camp_001", "game_id":"game_001",
  "goal": { "metric":"eggs_caught", "target": 1000 },          // sum of member contributions
  "team": { "max_members": 10, "min_members_to_claim": 3 },
  "reward_split": { "policy":"by_contribution",                 // equal | by_contribution | top_n
                    "prizes":[{"prize_id":"p_team","quantity":10}], "top_n": 3 } }

// GET /groups/{groupId} → data
{ "group_id":"grp_001", "name":"Team A", "invite_code":"AB12CD",
  "members":[{"player_id":"player_1","contribution":320}, …],
  "goal":{ "metric":"eggs_caught", "target":1000, "current":740, "remaining":260,
           "status":"in_progress" } }   // live via WS topic group:{group_id}
```

Flow: each member's `Play` emits a `contribution` event → atomically increments the team's shared
counter (Redis live + DB durable). On reaching `target` → `goal_reached` event → group reward
distributed per `reward_split` policy through the **fulfillment outbox** (idempotent, once per
team). Live progress pushed on WS topic `group:{group_id}`.

## 13. Realtime Match (synchronous multiplayer — `match` service, WS protocol)

Room management is REST (consumer BFF → `match` via gRPC); live play is a **bidirectional WS** to
the `match` service edge (sticky/sharded by `room_id`; token minted by the consumer BFF).

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/match/rooms` | Player | Create room → `{ room_id, join_code, ws_url, ws_token }` |
| POST | `/api/v1/match/rooms/join` | Player | Join by code → `{ room_id, ws_url, ws_token }` |
| GET | `/api/v1/match/rooms/{roomId}` | Player | Room snapshot (members, status) |
| POST | `/api/v1/match/queue` | Player | (optional) Matchmaking — auto-assign a room |
| WS | `/api/v1/match/ws?room_id=…&token=…` | Player | Authoritative live socket |

```jsonc
// client → server (intents only; server is authoritative)
{ "type":"action", "seq":12, "action":"push", "payload":{ "target":"player_456" } }
{ "type":"ready" }   { "type":"leave" }   { "type":"ping" }
// server → client
{ "type":"state",  "tick":240, "state":{ /* authoritative snapshot or delta */ } }
{ "type":"event",  "event":"player_joined"|"round_start"|"match_end", "data":{…} }
{ "type":"result", "ranking":[…], "rewards":[…] }   // on match_end → Core reward/leaderboard pipeline
```

Server-authoritative: validates each intent, applies on the tick, broadcasts deltas (never trusts
client outcomes). Room state in-memory + periodic Redis snapshot for crash recovery/reconnection;
on `match_end` results flow into the standard reward handler + fulfillment + leaderboard.

---

## Error Catalog (Google canonical codes + stable `reason`)

`code` is the canonical `google.rpc.Code` integer; `data.error.status` its name; `data.error.reason`
the stable machine string (domain `muse.game`); HTTP status follows Google's code↔HTTP mapping.

| `code` | status (name) | HTTP | `reason` | When |
|---|---|---|---|---|
| 3 | INVALID_ARGUMENT | 400 | `VALIDATION_FAILED` | Malformed body / bad params (`field_violations[]`) |
| 16 | UNAUTHENTICATED | 401 | `UNAUTHENTICATED` | Missing/invalid JWT or widget token |
| 7 | PERMISSION_DENIED | 403 | `PERMISSION_DENIED` | Role lacks permission |
| 5 | NOT_FOUND | 404 | `RESOURCE_NOT_FOUND` | Game/prize/campaign/session not found |
| 9 | FAILED_PRECONDITION | 400 | `SESSION_EXPIRED` | Play after `expires_at` |
| 9 | FAILED_PRECONDITION | 400 | `OUT_OF_TURNS` | No remaining plays / rule limit hit |
| 9 | FAILED_PRECONDITION | 400 | `INSUFFICIENT_ITEMS` | Redeem below milestone requirement |
| 10 | ABORTED | 409 | `PRIZE_OUT_OF_STOCK` | Prize stock exhausted at deduction time |
| 10 | ABORTED | 409 | `CONCURRENT_MODIFICATION` | Optimistic-lock/txn retry conflict |
| 6 | ALREADY_EXISTS | 409 | `REWARD_ALREADY_CLAIMED` | Reward already claimed/fulfilled |
| 6 | ALREADY_EXISTS | 409 | `CONTACT_CONFLICT` | Linking a phone/email already bound to another identity |
| 9 | FAILED_PRECONDITION | 400 | `GROUP_FULL` | Team at `max_members` |
| 9 | FAILED_PRECONDITION | 400 | `NOT_GROUP_MEMBER` | Caller not in the team |
| 9 | FAILED_PRECONDITION | 400 | `ROOM_FULL` / `ROOM_CLOSED` | Match room full or already started/ended |
| 3 | INVALID_ARGUMENT | 400 | `CHEAT_DETECTED` | Validator rejected payload (timing/score/drop-plan) |
| 8 | RESOURCE_EXHAUSTED | 429 | `RATE_LIMITED` | Token bucket exceeded (`Retry-After` header) |
| 13 | INTERNAL | 500 | `INTERNAL` | Unexpected error |

Example error body (full envelope):
```jsonc
{ "code": 10, "message": "All units of 'Voucher 100K' have been awarded.",
  "trace_id": "01HX3...", "data": {
    "error": { "status": "ABORTED", "reason": "PRIZE_OUT_OF_STOCK", "domain": "muse.game",
               "metadata": { "prize_id": "prize_001", "game_id": "game_001" } } } }
```

## Versioning & deprecation
- URL-path versioning (`/api/v1`). Additive changes (new optional fields, new endpoints) ship in
  v1. Breaking changes → `/api/v2` with both running in parallel ≥90 days; responses for the
  deprecated version carry `Deprecation: true` + `Sunset: <date>` headers.
- Game `handlerConfig` is versioned by `rewardHandler` name — a v2 handler is a new registry key
  (`probability_v2`), never a mutation of an existing handler's contract.

## OpenAPI deliverable
- **Two specs** — `bff-consumer/api/openapi.yaml` and `bff-admin/api/openapi.yaml` (OpenAPI 3.1) —
  share common components (factored into `bffkit/api/components.yaml`, referenced via `$ref`).
  Because the envelope is uniform, define a generic `Envelope` schema (`code`, `message`,
  `trace_id`, `data`) and compose per-endpoint responses with `allOf` overriding `data`; reusable
  `ErrorEnvelope`, `CursorPage` (`next_cursor`/`has_more`/`total`), and `ErrorInfo`
  (`status`/`reason`/`domain`/`metadata`/`field_violations`) schemas; security schemes `BearerAuth`
  (JWT) + `WidgetToken` (`X-Widget-Token`). CI gate: `npx @redocly/cli lint` on both specs; mock with
  `npx @stoplight/prism-cli mock <spec>` per service for contract tests.
