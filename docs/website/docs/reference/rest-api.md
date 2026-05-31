---
title: REST API
sidebar_position: 1
---

# REST API reference

All paths are versioned under `/api/v1`. Every response uses the envelope
`{ code, message, trace_id, data }`. Snake_case everywhere. IDs are prefixed opaque strings
(`game_…`, `prize_…`, `sess_…`, `player_…`, `camp_…`, …).

:::info Two REST surfaces
**Core (`:8090`)** serves every `game.v1` RPC as REST directly (grpc-gateway), 1:1 with the proto:
the request body is the proto message, the tenant/merchant **scope is a request field** (in the
body, or `?scope.tenant_id=…` on GETs), and there is **no auth** — Core trusts the caller. Run
`make smoke-rest` to see it.

The routes documented **below** are the **reference BFFs** (`examples/bff-consumer` `:8080`,
`examples/bff-admin` `:8081`). They add the developer-facing edge on top of Core: JWT/header auth,
RBAC, rate limiting, caching, and flatter view-models (e.g. `data.prize_id` instead of
`data.prize.id`). **Build your own the same way** with `bffkit` — these are a starting point to
copy, not a fixed contract.
:::

## Conventions

- **Auth** — `Authorization: Bearer <jwt>` (player or admin). Dev/legacy fallback headers:
  `X-Tenant-Id`, `X-Merchant-Id`, `X-Player-Id`, `X-Roles`.
- **Collections** — `data: { items: [...], pagination: { next_cursor, has_more } }`; cursor
  pagination (`?cursor=&limit=`, default 20, max 100); filters as query params.
- **Idempotency** — mutating gameplay/claim accept an `Idempotency-Key` header.
- **Rate limiting** — `/start` and `/play` are limited per player/IP; `429` returns `Retry-After`.
- **Timestamps** — every time field (`created_at`, `updated_at`, `start_date`, `expires_at`, …) is
  a unix timestamp in **seconds** (`0` = unset). These BFFs emit it as a JSON number; Core's direct
  REST encodes the underlying `int64` as a quoted string per proto3 JSON.
- **Enums** — closed-set fields (`status`, `metric`, `state`, `mode`, ledger `reason`, contact
  `type`, auth `method`, quest `type`, integration `type`, …) are typed proto enums and serialize
  as their value **name** on the wire — `"status": "CAMPAIGN_STATUS_ACTIVE"`, not `"active"`.
  Unset reads back as `*_UNSPECIFIED`. Open, registry-driven fields stay free-form strings: game
  `type`/`seed_generator`/`reward_handler`/`validator`, prize/reward `type`, and integration event
  types (`play_completed`, `prize_won`, …). The persistence layer still stores the bare lowercase
  token, so the data model's `won → claimed → …` notation refers to stored values, not the wire form.

## Consumer BFF (`:8080`) — public widget + player

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/games/{id}/start` | public/player | create session + seed |
| POST | `/games/{id}/play` | public/player | submit payload → rewards |
| GET | `/games/{id}/eligibility` | public/player | remaining turns / can-play |
| GET | `/games/{id}/history/me` | player | caller's play history |
| GET | `/public/campaigns/{id}` | public | widget render config (cached) |
| POST | `/players/auth/start` · `/players/auth/verify` | public | phone/email login (BFF-owned: challenge, verify, JWT mint; calls Core `ResolvePlayer`) |
| GET · PUT | `/players/me` | player | profile |
| POST | `/players/me/contacts` | player | link a contact |
| GET | `/players/me/turns` | player | turn balance |
| GET | `/games/{id}/eligibility` | player | turns / can-play |
| POST | `/quests/{id}/complete` | player | complete a quest → grant turns |
| GET | `/leaderboards/{id}/rankings` | public | top-N (cached) |
| GET | `/wallet` · `/wallet/ledger` | player | balances / movements |
| GET | `/games/{id}/milestones` | player | milestone progress |
| POST | `/games/{id}/redeem` | player | claim / exchange a milestone |
| GET | `/rewards/me` · POST `/rewards/{id}/claim` | player | own rewards |

## Admin BFF (`:8081`) — dashboard + machine callbacks

The management surface is **role-guarded** (`admin` / `designer` / `reward_manager`). The n8n
callback is HMAC-verified and sits outside the role gate.

| Method | Path | Purpose |
|---|---|---|
| POST/GET/PUT/DELETE | `/admin/games`, `/admin/games/{id}` | game config |
| POST/GET/PUT/DELETE | `/admin/prizes`, `/admin/prizes/{id}` | prizes |
| POST | `/admin/prizes/{id}/codes` | import a voucher-code pool |
| GET | `/admin/prizes/summary` | stock summary |
| POST | `/admin/rewards/{id}/fulfill` · `/revoke` | reward lifecycle |
| POST/GET/PUT | `/admin/campaigns`, `/admin/campaigns/{id}` | campaigns (+ `/duplicate`, `/analytics`) |
| POST/GET/PUT/DELETE | `/admin/quests` | quests |
| CRUD + finalize | `/admin/leaderboards`, `/admin/leaderboards/{id}` | leaderboards (+ `/finalize`, `/disqualify`, `/adjust`) |
| POST/GET/DELETE | `/admin/integrations` | outbound adapters |
| POST | `/admin/integrations/emit` | inject an event (test) |
| GET | `/admin/fulfillment/tasks` · POST `/{id}/retry` | outbox ops |
| POST | `/fulfillment/tasks/{id}/callback` | **machine** (HMAC) n8n callback |
| POST/GET | `/admin/tenants`, `/admin/merchants` | tenancy |

## Examples

```jsonc
// POST /games/{id}/play  (request)
{ "session_id": "sess_001", "payload": {} }                    // spin / scratch
{ "session_id": "sess_002", "payload": { "score": 75, "duration_ms": 8000 } } // egg-catcher
{ "session_id": "sess_003", "payload": { "caught_items": ["d_1","d_3"] } }    // gift-catcher

// data (reward_result) — same shape for every game
{ "rewards": [ { "prize_id": "p_001", "name": "Voucher 100K", "type": "voucher", "quantity": 1, "value": 100000 } ],
  "metadata": { "slot_index": 3 } }
```

For machine-readable errors, see the [Error reference](errors.md).

:::note OpenAPI
Per-BFF OpenAPI 3.1 specs (`bff-*/api/openapi.yaml`) are a planned addition; this page is the
current source of truth for the REST surface.
:::
