---
title: REST API
sidebar_position: 1
---

# REST API reference

All paths are versioned under `/api/v1`. Every response uses the envelope
`{ code, message, trace_id, data }`. Snake_case everywhere. IDs are prefixed opaque strings
(`game_…`, `prize_…`, `sess_…`, `player_…`, `camp_…`, …).

## Conventions

- **Auth** — `Authorization: Bearer <jwt>` (player or admin). Dev/legacy fallback headers:
  `X-Tenant-Id`, `X-Merchant-Id`, `X-Player-Id`, `X-Roles`.
- **Collections** — `data: { items: [...], pagination: { next_cursor, has_more } }`; cursor
  pagination (`?cursor=&limit=`, default 20, max 100); filters as query params.
- **Idempotency** — mutating gameplay/claim accept an `Idempotency-Key` header.
- **Rate limiting** — `/start` and `/play` are limited per player/IP; `429` returns `Retry-After`.

## Consumer BFF (`:8080`) — public widget + player

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/games/{id}/start` | public/player | create session + seed |
| POST | `/games/{id}/play` | public/player | submit payload → rewards |
| GET | `/games/{id}/eligibility` | public/player | remaining turns / can-play |
| GET | `/games/{id}/history/me` | player | caller's play history |
| GET | `/public/campaigns/{id}` | public | widget render config (cached) |
| POST | `/players/auth/start` · `/players/auth/verify` | public | phone/email login |
| GET · PUT | `/players/me` | player | profile |
| POST | `/players/me/contacts` | player | link a contact |
| GET | `/players/me/turns` | player | turn balance |
| GET | `/games/{id}/eligibility` | player | turns / can-play |
| POST | `/quests/{id}/complete` | player | complete a quest → grant turns |
| GET | `/leaderboards/{id}/rankings` | public | top-N (cached) |
| GET | `/leaderboards/{id}/around-me` · `/my-rank` | player | personalized rank |
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
