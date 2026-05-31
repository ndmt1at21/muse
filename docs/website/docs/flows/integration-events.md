---
title: Integration events
sidebar_position: 6
---

# Integration events: emit → fan-out → deliver

A domain event flows through the Hub to every subscribed integration, best-effort, without ever
affecting the operation that emitted it.

## End-to-end

```mermaid
sequenceDiagram
  autonumber
  participant Core as Core (engine/service)
  participant Hub as Integration Hub (EventSink)
  participant Bus as Redis pub/sub
  participant DB as SQL
  participant P as Provider (webhook/n8n/stub)
  participant Ext as External endpoint

  Note over Core: play/claim/quest/finalize committed
  Core->>Hub: Emit(event{type, scope, payload})
  Hub->>Bus: Publish (cross-process fan-out)
  Hub->>DB: list active integrations in scope subscribed to type
  loop each matching integration
    Hub->>P: Deliver(integration, event)
    P->>Ext: POST event JSON (X-Muse-Signature if configured)
    Ext-->>P: 2xx
    P-->>Hub: ok (or error → logged & skipped)
  end
  Note over Core,Hub: Emit returns nothing — failures never bubble to the caller
```

## Subscription matching

```mermaid
flowchart TD
  ev["event{ type, scope, payload.campaign_id? }"] --> q["query: active integrations<br/>in (tenant, merchant)<br/>campaign == payload.campaign_id OR campaign == ''"]
  q --> f{"integration.Subscribes(type)?"}
  f -->|yes| deliver["provider.Deliver"]
  f -->|no| skip["skip"]
```

An integration matches when it is **active**, in the event's tenant/merchant scope, subscribed to
the event type, and either scope-wide (`campaign_id == ""`) or narrowed to the event's campaign.

## Register + test

```mermaid
sequenceDiagram
  autonumber
  participant Adm as Dashboard
  participant A as Admin BFF
  participant Core as Core (IntegrationService)

  Adm->>A: POST /admin/integrations { type, events[], config }
  A->>Core: CreateIntegration (role-guarded)
  Core-->>A: { id, ... }

  Adm->>A: POST /admin/integrations/emit { type, payload }
  A->>Core: EmitEvent
  Core->>Core: Hub.Dispatch(event)
  Core-->>A: { dispatched: N }
  Note over Adm,A: N = how many integrations received the event
```

Proven by `make e2e-integration`: register an `sms` integration, emit `prize_won` → `dispatched: 1`;
emit an unsubscribed event → `dispatched: 0`; delete it → `dispatched: 0`.
