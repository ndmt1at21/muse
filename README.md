# Muse — Config-Driven Game Service

A generic, **config-driven game backend** for a website-builder platform. Adding a new
game of an existing *shape* needs **zero backend code** — just a JSON config; adding a new
*shape* needs only a small handler/validator, never engine changes.

The repo is a complete, runnable implementation: the generic engine, three game shapes,
rewards & fulfillment, tenancy/identity/players, wallet/points/exchange, campaigns, quests &
leaderboards, an outbound integration hub, and observability — all on **both Postgres and
MySQL** behind one uniform REST envelope. This README is organized by **capability**; the
internal build history lives in [PLAN.md](PLAN.md).

## 📖 Documentation site

A [Docusaurus](https://docusaurus.io/) site in [`docs/website/`](docs/website/) visualizes the
**architecture** and **flows** (Mermaid diagrams) and documents how to run and extend Muse —
the best starting point for new users.

```bash
make docs          # live docs at http://localhost:3000  (or: cd docs/website && npm install && npm start)
make docs-build    # static build → docs/website/build
```

Covers: architecture (components, topology, tenancy/identity, data model) · concepts (engine,
anti-cheat, rewards/fulfillment, wallet, quests/leaderboard, integrations) · **flow sequence
diagrams** (gameplay, auth, fulfillment, wallet, leaderboard, events) · guides (quickstart,
add-a-game, add-a-shape) · reference (REST API, errors, observability).

## CI & deployment

GitHub Actions in [`.github/workflows/`](.github/workflows/):

- **`ci.yml`** (push + PR) — `go` job (gofmt check, build, vet, unit tests, and the gamekit
  race test across both modules), an `integration` job (adapter port-contract suite against real
  Postgres + MySQL + Redis via testcontainers), and a `docs` job that builds the Docusaurus site.
- **`docs.yml`** (push to `main` touching `docs/website/`) — builds the docs and deploys them to
  **GitHub Pages**. The Pages base path is injected at build time, so it works for project pages
  (`https://<owner>.github.io/<repo>/`) with no config edits.

> One-time setup: in the repo **Settings → Pages**, set **Source = GitHub Actions**.

## Two consumption modes

The engine ships as a pure SDK so it can be consumed two ways:

- **Mode A — embed the SDK** (`gamekit`): import the engine, register handlers, call
  `engine.Play(...)` directly. Depends only on **port interfaces**; bring your own DB/transport
  or use our `adapters`. No dependency on Core/proto. See [`examples/embed`](examples/embed).
- **Mode B — run the hosted API**: deploy `core`, the central product surface. Core serves the
  full `game.v1` contract over **both gRPC and REST** (a grpc-gateway under `/api/v1`, wrapped in
  the uniform `{code, message, trace_id, data}` envelope), so it is usable directly. Core is
  **auth-agnostic** — it trusts the caller to authenticate and to pass the tenant/merchant scope,
  and only validates the business object. **You design your own BFF** for the edge concerns
  (auth, RBAC, rate limiting, caching, view-model assembly); `bffkit` is the toolkit for that, and
  [`examples/bff-consumer`](examples/bff-consumer) + [`examples/bff-admin`](examples/bff-admin) are
  runnable reference BFFs to copy and adapt.

```
gamekit (no I/O, no transport)  →  adapters (SQL/Redis port impls)  →  core (wire + gRPC + REST)
                                                                          │
                                                          your BFF (bffkit) ── examples/bff-*
```

## Layout

| Path | What |
|---|---|
| `gamekit/` | **Pure engine SDK** — its own Go module, stdlib-only. `types`, `ports`, `engine`, `registry`, built-in `handlers`/`seeds`/`validators`, `memstore` (in-memory ports), `std` (registry preload). |
| `proto/`, `pkg/gen/` | Protobuf contracts (`game.v1`) + generated Go (buf). |
| `pkg/apierr`, `pkg/dialect` | Error model (gkerr ↔ gRPC/HTTP) and the SQL dialect abstraction. |
| `adapters/sqlstore` | Default port impls over **Postgres + MySQL** (no ORM, raw SQL, atomic `Deduct`, goose migrations). |
| `adapters/redisstore` | Ephemeral state over Redis/Dragonfly (idempotency cache; sessions/leaderboard later). |
| `core/` | The product surface: wires gamekit + adapters, maps proto ↔ engine, serves **gRPC + REST** (grpc-gateway, enveloped). Auth-agnostic. |
| `pkg/envelope` | The uniform REST envelope + gRPC-status→error mapping, shared by Core's gateway and any BFF. |
| `bffkit/` | The **BFF toolkit** for building your own edge: `envelope`, `coreclient`, `auth`/RBAC seam, `ratelimit`, `cache`, `obs`, `middleware`. |
| `examples/bff-consumer/`, `examples/bff-admin/` | **Reference** REST BFFs (public widget vs internal admin) built on `bffkit` — copy & adapt. |

## Quick start

### Mode A — pure SDK, no infrastructure
```bash
make embed        # runs examples/embed: Start → Play, entirely in memory
make test         # gamekit unit tests (incl. the concurrency stock-race test)
make test-race
```

### Integration tests (real engines via testcontainers)
The adapter port-contract suite spins up **real Postgres, MySQL, and Redis**
containers (testcontainers-go) and runs one shared suite against both SQL
engines — game/prize/session/history roundtrips, scope isolation, and a
concurrent stock-race through the full engine `Play` transaction proving no
over-issue. Requires a running Docker daemon.
```bash
make test-integration     # go test -tags integration ./adapters/...
```

### Mode B — hosted API end-to-end
```bash
make up           # docker compose: postgres + mysql + dragonfly + core + both BFFs + prometheus + grafana
make seed         # demo data: campaign + spin-wheel game + prizes + integration, then one play
make e2e          # scripted spin-wheel flow: create prize+game → start → play → history
make down
```
Observability: **Grafana** at http://localhost:3000 (anonymous admin) with a provisioned
"Muse — Overview" dashboard, **Prometheus** at http://localhost:9092. Each service exposes `/metrics`.

Run services locally instead of in containers:
```bash
make up-data                      # just the datastores
make migrate                      # apply schema to Postgres
make run-core                     # terminal 1
make run-consumer                 # terminal 2
make run-admin                    # terminal 3
./deploy/e2e.sh                   # terminal 4
```

Target MySQL instead: `make migrate-mysql` + `make run-core-mysql`.

## API surface

Uniform envelope on every response: `{ code, message, trace_id, data }`. Errors follow the
Google API error model (canonical code + stable `reason` + `ErrorInfo`).

**Core REST (`:8090`)** — every `game.v1` RPC is exposed as JSON/HTTP under `/api/v1` by the
grpc-gateway, enveloped identically. Because Core is auth-agnostic, the tenant/merchant scope
travels as ordinary request fields (in the JSON body, or `?scope.tenant_id=…` on GETs) rather than
the `X-Tenant-Id` headers a BFF translates. Try it with `make smoke-rest` (Core running). This is
the surface your own BFF — or a direct integration — calls.

The routes below are the **reference BFFs** (`examples/`): they front Core REST/gRPC with auth, the
header seam, RBAC, rate limiting, caching, and flatter view-models. Build your own the same way.

Consumer BFF (`:8080`):
- `POST /api/v1/games/{gameId}/start` — create session + seed
- `POST /api/v1/games/{gameId}/play` — submit payload → rewards (atomic stock deduction)
- `GET  /api/v1/games/{gameId}/eligibility` — remaining turns / can-play
- `GET  /api/v1/games/{gameId}/history/me` — caller's play history (paginated)
- `GET  /api/v1/games/{gameId}/render` — per-game presentation config (opaque `ui`: background, theme, slot/item images; redacted — never odds)
- `GET  /api/v1/wallet?scope_key=…` — player's per-currency balances (player JWT)
- `GET  /api/v1/wallet/ledger?scope_key=…` — wallet movements, newest-first (player JWT)
- `GET  /api/v1/games/{gameId}/milestones` — milestone progress + per-rung status (player JWT)
- `POST /api/v1/games/{gameId}/redeem` — claim/exchange a milestone for its prize (player JWT)

Admin BFF (`:8081`):
- `POST /api/v1/admin/games`, `GET /api/v1/admin/games/{gameId}`, `PUT /api/v1/admin/games/{gameId}` (update config incl. the `ui` render block)
- `POST /api/v1/admin/prizes`

> Auth is a seam: a player JWT (Bearer) is verified into request claims when present,
> falling back to the `X-Tenant-Id` / `X-Merchant-Id` / `X-Player-Id` / `X-Roles` headers — handler
> code is unchanged either way. The admin management surface is **role-guarded**:
> requests need an `admin` / `designer` / `reward_manager` role (from the admin JWT, or the
> `X-Roles` dev header); the HMAC-signed n8n callback stays outside the role gate.

## Game shapes (config only — no engine changes)

Each shape is a combination of registered `seed_generator` + `reward_handler` + `validator`:

| Shape | seed | handler | validator | Play payload |
|---|---|---|---|---|
| `spin_wheel` / `scratch_card` | `none` | `probability` | `basic` | `{}` |
| `egg_catcher` | `none` | `score_to_tier` | `time_and_score_range` | `{"score":75,"duration_ms":8000}` |
| `gift_catcher` | `drop_sequence` | `collect_items` | `drop_plan` | `{"caught_items":["d_3","d_6"]}` |

Adding a shape of an existing combination = JSON config only. A genuinely new shape =
register one handler/seed/validator; the engine core never changes.

```bash
make e2e             # spin-wheel flow
make e2e-shapes      # egg-catcher (tier award + cheat ceiling) + gift-catcher (drop plan + over-catch)
make e2e-rewards     # reward system: per-user cap, code assignment, claim → fulfill → revoke lifecycle
make e2e-fulfillment # outbox + dispatcher delivery, dead-letter/retry, HMAC-signed n8n callback
make e2e-identity    # identity: one identity across tenants, isolated players, OTP login, JWT, contact linking
make e2e-wallet      # wallet: lucky_item credit, balance/ledger, cumulative_unlock milestone redeem (grant-once)
make e2e-hardening   # BFF hardening: admin RBAC 403/200, read-model cache + invalidation, gameplay rate-limit 429
make e2e-integration # integration hub: register adapters, emit events, fan-out dispatch counts
```

## Reward system

Prizes carry **constraints** (`max_per_user`/`max_per_day`, enforced inside the Play txn —
a capped prize the wheel lands on is dropped to no-win) and **fulfillment** policy
(`redemption_mode`: instant/on_claim/manual/exchange; `method`: code/...). Each awarded unit
becomes a durable **reward record** with a `won → claimed → fulfilled → revoked` lifecycle;
voucher prizes pop a **code** from an imported pool at win time. Admin endpoints cover prize
CRUD, code import, stock summary, and `fulfill`/`revoke`; players `list`/`claim` their rewards.

## Tenancy, identity & players

Three layers model "the same person plays in many tenants, fully isolated":

- **Tenancy** — `Tenant` (platform org) → `Merchant` (brand/store) → Campaign → Game. Every row
  carries `tenant_id`; `wallet_scope` (campaign|merchant|tenant) is a tenant setting.
- **Global identity** — an `identities` row is a real person identified by **verified contacts**
  (`phone` normalized E.164-ish, `email` lowercased), **each globally unique**. A new contact that
  already maps to an identity links to that person; otherwise it creates one.
- **Tenant-scoped player** — `UNIQUE(tenant_id, identity_id)`. The same phone → **one identity** but
  a **different, isolated `player_id` per tenant**; profile, turns, wallet, and history hang off the
  player.

Login accepts **phone or email** via pluggable methods (`code`/`otp`/`magic_link`/`social` — dev
stubs now, real providers swap in behind the `Method` seam). `VerifyAuth` resolves-or-creates the
identity, upserts the tenant player, and issues a **player JWT** (`pkg/token`, HS256) carrying
`tenant_id`/`merchant_id`/`player_id`/`identity_id`. The BFF `auth` seam verifies the Bearer token
into request claims, falling back to the `X-Tenant-Id`/`X-Player-Id` headers so existing callers
keep working. The pure resolver lives in `gamekit/identity` (ports-only, embeddable); `TenantStore`/
`IdentityStore`/`PlayerStore` let an embedder plug their own user system.

## Fulfillment & delivery

Delivery is **configured per prize, not coded per prize**. A prize's `fulfillment.channel`
(`voucher_code` | `sms` | `zns` | `email` | `points_credit` | `physical_shipping` | `crm_sync` |
`ecommerce` | `external_workflow`) selects a **`FulfillmentProvider`** (a pluggable registry,
same pattern as reward handlers). In-app channels (`voucher_code`/none) are delivered
synchronously at win; every other channel hands off out-of-band:

- **Transactional outbox** — a `fulfillment_tasks` row is written in the **same DB txn** as the
  reward + stock deduction (at win for `instant`, at claim for `on_claim`), so a delivery is
  never lost or duplicated.
- **Dispatcher worker** — polls due tasks (`SELECT … FOR UPDATE SKIP LOCKED`, multi-replica
  safe), invokes the provider with **exponential backoff**, and moves a task to a **dead-letter**
  state after its attempt budget: `pending → processing → fulfilled | failed | dead`. Completing
  a task flips its reward to `fulfilled`.
- **n8n via `external_workflow`** — the provider POSTs the task to a per-prize webhook with an
  **HMAC-SHA256 signature**; n8n runs the no-code flow and calls back the signed
  `POST /api/v1/fulfillment/tasks/{id}/callback` (verified at the admin BFF edge) to report
  `fulfilled`/`failed` with a receipt.
- **Admin** — `GET /api/v1/admin/fulfillment/tasks` (filter status/campaign/prize) and
  `POST /api/v1/admin/fulfillment/tasks/{id}/retry` re-arms a failed/dead task.

## Wallet, points & exchange

Some games award an intermediate **wallet currency** instead of (or alongside) a real prize: the
`lucky_item` handler credits a named item, and any handler's `points` reward credits points. The
engine routes these `points`/`lucky_item` rewards to the **wallet ledger** inside the Play txn (never
stock-deducted or fulfilled), so balances **accumulate across plays**.

- **Scope** — balances are keyed by `(tenant_id, scope_key, currency)` where `scope_key` follows the
  game's `wallet_scope` (`campaign` default | `merchant` | `tenant`), so the same schema backs one-off
  events and brand-loyalty wallets without change.
- **Milestones** convert accumulated balance into prizes, two modes: `cumulative_unlock` (reach the
  threshold → grant once; `auto_grant` grants inside Play, else the player claims via `redeem`) and
  `spend_exchange` (`redeem` atomically spends the threshold from the balance). Granting is **once-only**
  (a second redeem returns `ALREADY_EXISTS`), and the milestone prize mints a durable reward record
  (+ fulfillment task when async), mirroring the engine's award path.
- **Player surface** — `GET /wallet` (balances), `GET /wallet/ledger` (movements), `GET
  /games/{id}/milestones` (progress + per-rung status), `POST /games/{id}/redeem`.

## BFF hardening

These are **edge concerns that live in the BFF, not Core** — `bffkit` provides them so your own
BFF (and the reference ones in `examples/`) stay consistent. All are **optional/degrading** —
without Redis, rate limiting and caching become no-ops and gameplay still works (correctness-critical
limits like stock stay in Core, which enforces them regardless of which BFF, or no BFF, fronts it).

- **Distributed rate limiting** — `/start` and `/play` sit behind a Redis fixed-window counter
  (atomic Lua INCR+PEXPIRE, so the limit holds across BFF replicas), keyed per player (or per IP for
  anonymous/widget traffic). Over-limit returns `429` `RATE_LIMITED` with a `Retry-After` header.
  Limit is `PLAY_RATE_LIMIT`/min (default 60).
- **Read-model cache** (`bffkit/cache`) — cache-aside over the shared `muse:bff` Redis namespace for
  the assembled view models: the public widget config (`/public/campaigns/{id}`, ~60s TTL + explicit
  invalidation) and the top-N leaderboard (`/leaderboards/{id}/rankings`, short TTL, no bust). The
  admin BFF **busts** the public config key on `UpdateCampaign`, so a config change shows up at the
  edge immediately even though both BFFs are separate processes (they share one Redis).
- **Role-based admin authz** — the admin JWT carries `roles`; the management surface (games, prizes,
  campaigns, tenants, quests, leaderboards) requires an `admin` / `designer` / `reward_manager` role
  via `auth.RequireRole`. The signed n8n fulfillment callback is a machine route and stays outside
  the gate. A `X-Roles` dev header mirrors the existing header seam for local/e2e.

## Integration hub

Domain events fan out to **outbound integrations** so a campaign can push wins to a webhook, an n8n
flow, a Google Sheet, SMS/ZNS, or a CRM — configured, not coded.

- **Events** — the engine and hosting services emit canonical events: `play_completed`, `prize_won`
  (engine, post-commit), `prize_claimed` (reward claim), `quest_completed` (quest complete),
  `leaderboard_finalized` (finalize). All best-effort — an integration never fails the play/claim.
- **Hub** (`core/internal/integration`) is the engine's `EventSink`. On each event it (1) publishes
  to the **Redis pub/sub bus** (`adapters/events`) for cross-process fan-out (extra replicas, a
  future realtime gateway), and (2) looks up the active integrations in the event's scope that
  **subscribe** to that type (campaign-narrowed or scope-wide) and delivers through their providers.
- **Providers** are a pluggable registry (same pattern as reward handlers / fulfillment channels):
  a real **webhook** (JSON POST, optional HMAC-SHA256 `X-Muse-Signature` — also serves `n8n`), plus
  logging **stubs** for `gsheet` / `sms` / `zns` / `crm` (swap in a real impl by re-registering the
  type). Delivery failures are logged and skipped, never retried into the caller.
- **Admin** — `POST/GET/DELETE /api/v1/admin/integrations` (role-guarded) register/list/delete
  adapters; `POST /api/v1/admin/integrations/emit` injects an event for testing a wiring and returns
  the dispatch count. `IntegrationService` is the Core gRPC contract.

## Observability

Prometheus metrics on every service, scraped into a provisioned Grafana dashboard with alert rules.

- **Core** (`/metrics` on the health port) — gRPC **RED** (rate/errors/duration per method, via an
  interceptor), **business counters** fed from domain events (`game_events_total{type}` — the
  gameplay funnel: play/win/claim/quest/finalize) through a `metricsSink` in the engine's EventSink
  chain, and `fulfillment_tasks_total{outcome}` from the dispatcher (delivered/awaiting/retry/dead).
- **BFFs** (`bffkit/obs`) — HTTP **RED** middleware keyed by the matched chi route pattern (bounded
  cardinality), plus `bff_cache_ops_total{result}` (read-model cache hit/miss). 429s and 5xx show up
  in the same `http_requests_total` series.
- **Grafana + Prometheus** are provisioned in `deploy/` (datasource, the overview dashboard, and
  `alerts.yml`: gRPC/HTTP error-rate & p99 SLO burn, fulfillment dead-letter growth, out-of-stock and
  rejected-play spikes). `make seed` produces traffic to populate them.
- **Error reference**: [docs/ERRORS.md](docs/ERRORS.md) — the canonical `reason` → gRPC code → HTTP
  status table (the stable machine-readable contract), generated from `gkerr`/`apierr`.

> Distributed tracing (Tempo/Loki, OTLP) and per-BFF OpenAPI specs are open items; metrics +
> dashboards + the error reference + seed data are done.

## What's proven here
- Generic engine (`Start`/`Play`/`Eligibility`/`History`) driven entirely by config.
- Three game shapes wired purely from registered handlers/seeds/validators.
- **Server-authoritative anti-cheat**: server-issued drop sequences (`drop_sequence`),
  drop-plan validation (unknown/duplicate/over-catch → `CHEAT_DETECTED`), score/duration ceilings.
- **Atomic stock deduction** under concurrency (race-tested), single-use sessions, turn caps;
  `collect_items` deducts per caught unit.
- Dual-engine persistence (Postgres + MySQL) behind one set of raw SQL queries.
- Tenant/merchant scope threaded through every port and query.
- **Wallet routing** in Play: `points`/`lucky_item` credited to the ledger, milestones → prizes.
- **BFF hardening**: distributed rate limiting, read-model cache + cross-service invalidation, admin RBAC.
- **Integration hub**: domain events fan out to pluggable outbound adapters over a Redis pub/sub bus.
- **Observability**: Prometheus RED + business metrics on every service, provisioned Grafana + alerts.
- **Per-game theming**: an opaque `ui` block (background, theme colors, slot/item images) stored on the game, editable any time, rendered by the widget via a redacted `/render` endpoint — never an engine change.

Everything above is implemented and runnable — engine, the three game shapes, rewards,
fulfillment, tenancy/identity/players, campaigns, quests, leaderboards, wallet/points/exchange,
BFF hardening, the integration hub, and observability + docs. On the roadmap: distributed tracing
(Tempo/Loki) and per-BFF OpenAPI specs, a realtime gateway, and multiplayer.
