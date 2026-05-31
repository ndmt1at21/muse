---
title: Player auth (OTP)
sidebar_position: 2
---

# Player auth: phone/email login

Login accepts **phone or email** via pluggable methods. Verification resolves-or-creates the global
identity, upserts the tenant player, and issues a player JWT. The same phone in two tenants → one
identity, two isolated players.

```mermaid
sequenceDiagram
  autonumber
  participant W as Widget
  participant C as Consumer BFF
  participant Core as Core (PlayerService)
  participant DB as SQL

  W->>C: POST /players/auth/start { identifier: {type, value}, method: "otp" }
  C->>Core: StartAuth(scope, contact, method)
  Core->>DB: create challenge (code, expires)
  Core-->>C: { challenge_id, dev_code? }
  Note right of Core: dev_code is returned only in AUTH_DEV_MODE
  C-->>W: 200 { challenge_id }

  W->>C: POST /players/auth/verify { challenge_id, code }
  C->>Core: VerifyAuth(challenge_id, code)
  Core->>DB: validate challenge
  Core->>DB: resolve-or-create identity by verified contact
  Core->>DB: upsert player UNIQUE(tenant_id, identity_id)
  Core->>Core: sign player JWT (tenant/merchant/player/identity)
  Core-->>C: { token, player }
  C-->>W: 200 { token, player }
```

## Resolve-or-create & contact linking

```mermaid
flowchart TD
  v["verified contact (phone/email)"] --> q{"contact already maps<br/>to an identity?"}
  q -->|yes| link["use that identity (merge)"]
  q -->|no| create["create a new identity"]
  link --> upsert["upsert player for (tenant, identity)"]
  create --> upsert
  upsert --> jwt["issue player JWT"]
```

## Using the token

The BFF auth seam verifies a `Bearer` token into request claims when present; routes that require a
player use `RequirePlayer`. For dev/e2e without a token, the `X-Tenant-Id` / `X-Player-Id` headers
are honored — handler code is identical.

```mermaid
sequenceDiagram
  participant W as Widget
  participant C as Consumer BFF
  W->>C: GET /players/me  (Authorization: Bearer <jwt>)
  C->>C: verify JWT → claims (tenant, player, identity)
  C->>C: scope resolved from claims
  C-->>W: 200 { profile, contacts }
  Note over C: no token on a RequirePlayer route → 401 UNAUTHENTICATED
```

Proven by `make e2e-identity`: the same phone in tenant A and tenant B yields **one** `identity_id`
but **different** `player_id`s, fully isolated.
