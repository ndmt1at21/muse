---
title: Quickstart
sidebar_position: 1
---

# Quickstart

Run the whole stack and play a spin wheel in a few minutes.

## Prerequisites

- Docker + Docker Compose
- `curl` and `jq` (for the scripted flows)
- Go 1.25+ (only if you want to run services outside containers)

## 1. Bring it up

```bash
make up        # postgres + mysql + dragonfly + core + reference BFFs + prometheus + grafana
```

This builds the images, applies migrations on boot, and starts everything. The endpoints:

| URL | What |
|---|---|
| `http://localhost:8090` | **Core REST** (`/api/v1`, no auth — the product surface; `make smoke-rest`) |
| `http://localhost:8080` | Consumer BFF — *reference* (`examples/`, widget + player) |
| `http://localhost:8081` | Admin BFF — *reference* (`examples/`, dashboard + callbacks) |
| `http://localhost:3000` | Grafana (anonymous admin) |
| `http://localhost:9092` | Prometheus |

> The BFF endpoints below are the **reference** edge (auth + view-models). Core also serves the
> same operations directly at `:8090` — that's the surface you build your own BFF against.

## 2. Seed a demo game

```bash
make seed      # creates a campaign + spin-wheel game + prizes + an integration, then plays once
```

Note the printed **game id** — you'll use it below.

## 3. Play it by hand

```bash
GAME=game_xxx   # from `make seed`
H='-H Content-Type:application/json -H X-Tenant-Id:tenant_demo -H X-Merchant-Id:merchant_demo -H X-Player-Id:alice'

# start → session_id
SID=$(curl -s $H -X POST localhost:8080/api/v1/games/$GAME/start -d '{}' | jq -r .data.session_id)

# play → reward
curl -s $H -X POST localhost:8080/api/v1/games/$GAME/play \
  -d "{\"session_id\":\"$SID\",\"payload\":{}}" | jq .data

# my history
curl -s $H localhost:8080/api/v1/games/$GAME/history/me | jq .data
```

Every response is the uniform envelope `{ code, message, trace_id, data }`.

## 4. Watch the metrics

Open Grafana → **Muse — Overview**. Generate some traffic and you'll see the gRPC/HTTP RED panels,
the gameplay funnel (`game_events_total`), fulfillment outcomes, and the cache hit ratio move.

## Scripted end-to-end flows

Each phase ships a runnable script (services must be up):

```bash
make e2e             # spin-wheel: create prize+game → start → play → history
make e2e-shapes      # egg-catcher + gift-catcher (tiers, drop plan, anti-cheat)
make e2e-rewards     # caps, code assignment, claim → fulfill → revoke
make e2e-fulfillment # outbox + dispatcher + dead-letter/retry + HMAC n8n callback
make e2e-identity    # one identity across tenants, isolated players, OTP login, JWT
make e2e-wallet      # lucky_item credit, balance/ledger, milestone redeem (grant-once)
make e2e-hardening   # admin RBAC 403/200, read-model cache, gameplay rate-limit 429
make e2e-integration # register adapters, emit events, fan-out dispatch counts
```

## Run without containers

```bash
make up-data         # just postgres + mysql + dragonfly
make migrate         # apply schema to Postgres   (make migrate-mysql for MySQL)
make run-core        # terminal 1
make run-consumer    # terminal 2
make run-admin       # terminal 3
```

## Tear down

```bash
make down
```

Next: **[Add a game](add-a-game.md)** (config only) or **[Add a shape](add-a-shape.md)** (one handler).
