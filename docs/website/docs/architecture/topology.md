---
title: Runtime topology
sidebar_position: 2
---

# Runtime topology

What actually runs when you `make up`. Everything is in `deploy/docker-compose.yml`.

```mermaid
flowchart TB
  subgraph edge["Reference BFFs (examples/) — your edge layer"]
    cons["bff-consumer<br/>:8080"]
    adm["bff-admin<br/>:8081"]
  end
  core["core — product surface<br/>:9090 gRPC · :8090 REST · :9091 health/metrics"]

  subgraph data["Stateful backends"]
    pg[("Postgres :5432")]
    my[("MySQL :3306")]
    df[("Dragonfly :6379<br/>Redis-compatible")]
  end

  subgraph obs["Observability"]
    prom["Prometheus :9092"]
    graf["Grafana :3000"]
  end

  cons -->|gRPC| core
  adm -->|gRPC| core
  client["direct REST client<br/>(own auth)"] -->|"/api/v1"| core
  core --> pg
  core -. "or" .- my
  core --> df
  cons --> df
  adm --> df
  prom -->|"scrape /metrics"| core
  prom -->|"scrape /metrics"| cons
  prom -->|"scrape /metrics"| adm
  graf -->|"query"| prom
```

## Services

| Service | Port(s) | Role |
|---|---|---|
| **core** | `9090` gRPC, `8090` REST, `9091` health+metrics | The product surface: engine + persistence + all hosting services, served over gRPC **and** REST (grpc-gateway, enveloped). The only thing that touches SQL. Auth-agnostic. |
| **bff-consumer** *(reference, `examples/`)* | `8080` | Public widget + player REST. CORS, per-player/IP rate limit, read-model cache. A reference for the public edge **you** build. |
| **bff-admin** *(reference, `examples/`)* | `8081` | Internal dashboard REST + the signed n8n callback. Role-based authz. A reference for the internal edge **you** build. |
| **postgres / mysql** | `5432` / `3306` | Durable system of record. Core targets **one** engine per deployment (`DB_ENGINE`); both are supported by the same raw SQL via a dialect layer. |
| **dragonfly** | `6379` | Redis-compatible: play sessions (TTL), idempotency keys, distributed rate-limit, leaderboard sorted sets, BFF read-model cache, and the event pub/sub bus. **Never** the source of truth for money/stock. |
| **prometheus** | `9092` | Scrapes `/metrics` from all three services; loads `alerts.yml`. |
| **grafana** | `3000` | Provisioned datasource + the "Muse — Overview" dashboard. |

## What lives where

```mermaid
flowchart LR
  subgraph SQL["SQL — durable truth"]
    s1["games · prizes · rewards"]
    s2["play_history · sessions (audit)"]
    s3["tenants · merchants · identities · players"]
    s4["campaigns · quests · leaderboards"]
    s5["wallet ledger · milestones"]
    s6["fulfillment_tasks (outbox)"]
    s7["integrations"]
  end
  subgraph Redis["Redis / Dragonfly — fast & ephemeral"]
    r1["sessions (TTL)"]
    r2["idempotency keys"]
    r3["rate-limit counters"]
    r4["leaderboard ZSET (live rank)"]
    r5["read-model cache (BFF)"]
    r6["event pub/sub bus"]
  end
```

**Rule of thumb:** anything involving money or stock is guarded by an atomic SQL conditional
update; Redis is cache + ephemeral state only. If Redis disappears, gameplay still works — caching
and rate-limiting simply degrade to no-ops.

## Health & readiness

- `GET /healthz` — liveness (always 200 if the process is up).
- `GET /readyz` — readiness; checks DB + Redis reachability (returns 503 with `X-Failed-Check`).
- `GET /metrics` — Prometheus exposition (see [Observability](../reference/observability.md)).
