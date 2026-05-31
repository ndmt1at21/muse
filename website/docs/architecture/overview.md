---
title: Overview
sidebar_position: 1
---

# Architecture overview

Muse is a Go monorepo (`go.work`) with a strict, one-directional dependency flow. Each layer only
knows about the layer beneath it.

```mermaid
flowchart TD
  subgraph SDK["gamekit — pure SDK (its own module, stdlib-only)"]
    types["types<br/>domain models"]
    ports["ports<br/>interfaces (Store, Cache, Locker, Clock…)"]
    engine["engine<br/>Start · Play · Eligibility"]
    reg["registry<br/>handlers · seeds · validators"]
  end

  subgraph Adapters["adapters — provided port impls"]
    sql["sqlstore<br/>Postgres + MySQL (no ORM)"]
    redis["redisstore<br/>sessions · cache · rate-limit · ZSET"]
    bus["events<br/>Redis pub/sub bus"]
  end

  subgraph CoreSvc["core — the product surface (gRPC + REST)"]
    grpc["grpcsvc<br/>proto ↔ engine"]
    gw["restgw<br/>grpc-gateway · enveloped"]
    svcs["services<br/>player · leaderboard · wallet · fulfillment · integration"]
    plat["platform<br/>config · health · metrics"]
  end

  subgraph BFF["BFF — the developer's edge (bffkit toolkit + examples/)"]
    kit["bffkit<br/>envelope · auth · cache · ratelimit · obs"]
    cons["examples/bff-consumer<br/>widget + player (reference)"]
    adm["examples/bff-admin<br/>dashboard + callbacks (reference)"]
  end

  engine --> ports
  reg --> ports
  engine --> reg
  sql -.implements.-> ports
  redis -.implements.-> ports
  grpc --> engine
  gw --> grpc
  svcs --> sql
  svcs --> redis
  grpc --> svcs
  cons --> kit
  adm --> kit
  cons -->|gRPC| grpc
  adm -->|gRPC| grpc

  classDef surface fill:#bbf7d0,stroke:#15803d,color:#052e16;
  class grpc,gw surface;

  classDef pure fill:#ddd6fe,stroke:#6d28d9,color:#1e1b4b;
  class types,ports,engine,reg pure;
```

## The layers

| Layer | Module(s) | Responsibility | Knows about… |
|---|---|---|---|
| **Pure SDK** | `gamekit` | Domain models + the generic engine + the handler/seed/validator registry. **No I/O.** | nothing but Go stdlib |
| **Adapters** | `adapters/sqlstore`, `adapters/redisstore`, `adapters/events` | Concrete implementations of the SDK's `ports` over SQL / Redis. | `gamekit/ports` |
| **Core** | `core` | The product surface. Wires `gamekit` + `adapters`, exposes the contract over **gRPC and REST** (grpc-gateway, enveloped), and hosts the services that need I/O (player auth, leaderboard, wallet, fulfillment, integration hub). **Auth-agnostic.** | gamekit + adapters + proto |
| **BFF** | `bffkit` (toolkit) + `examples/bff-consumer`, `examples/bff-admin` (reference) | The developer's edge, built on `bffkit`: auth, RBAC, caching, rate-limit, view-model assembly. **Not a shipped tier** — you build your own. | Core (via gRPC or REST) |

## Responsibility boundary: Core = business, BFF = presentation

This split is deliberate and load-bearing:

- **Core owns business objects & rules only.** It returns whole domain entities and business
  errors. The `ui` block on a game is an **opaque JSON blob** Core stores and returns but never
  interprets. Core does **not** authenticate — it trusts the caller to pass the tenant/merchant
  scope and only validates the business object. Core *does* now own the **transport-level**
  presentation its REST gateway needs: the envelope and the gRPC-status → HTTP mapping (shared via
  `pkg/envelope`), so a direct REST caller gets a first-class response without a BFF.
- **The BFF owns everything audience-facing.** It authenticates callers, enforces RBAC, aggregates
  multiple Core RPCs into one view model, shapes & redacts per audience (a public prize list strips
  `probability`/`stock`), caches hot reads, and rate-limits. This is **your** layer — `bffkit` is
  the toolkit, and `examples/` are runnable references to copy.

> **Consequence:** a new UI requirement is a BFF/widget change, not a Core change — reinforcing the
> config-driven goal. And because Core speaks REST directly, a simple integration can skip the BFF
> entirely and call `/api/v1` with its own auth in front.

## Why two reference BFFs?

```mermaid
flowchart LR
  subgraph Public["Public internet"]
    widget["Widget / player"]
  end
  subgraph Internal["Internal / VPN"]
    dash["Dashboard / staff"]
    n8n["n8n / orchestrator"]
  end

  widget -->|"CORS · rate-limit · player JWT"| C["examples/bff-consumer :8080"]
  dash -->|"role-based authz"| A["examples/bff-admin :8081"]
  n8n -->|"HMAC-signed callback"| A
  C -->|gRPC| Core[("Core :9090 gRPC / :8090 REST")]
  A -->|gRPC| Core
  direct["direct integration<br/>(own auth)"] -->|REST| Core
```

The references split by audience to show the pattern: each can scale and be secured independently —
the admin surface never sits on the public edge. Both import the shared **`bffkit`** so behavior
(envelope, auth seam, error mapping) stays consistent. **You're free to structure your own BFF
differently** — one service, three, or none (call Core's REST directly behind your own gateway).

## The uniform response envelope

Every REST response — success or error — has the same shape:

```jsonc
{ "code": 0,            // canonical google.rpc.Code; 0 = OK
  "message": "ok",
  "trace_id": "01H…",   // also echoed in X-Trace-Id
  "data": { /* payload on success; null on error */ } }
```

Errors carry a stable machine-readable `reason` in `data.error` — see the
[Error reference](../reference/errors.md).

## The uniform engine API

Everything a game does goes through three operations, regardless of shape:

```mermaid
flowchart LR
  S["Start(gameId, userId)"] -->|"session + seed"| P["Play(sessionId, payload)"]
  P -->|"rewards + metadata"| done([result])
  E["Eligibility(gameId, userId)"] -->|"can_play, remaining"| done
```

See **[Concepts → Generic engine](../concepts/generic-engine.md)** for how config selects the
behavior, and **[Flows → Gameplay](../flows/gameplay.md)** for the step-by-step sequence.
