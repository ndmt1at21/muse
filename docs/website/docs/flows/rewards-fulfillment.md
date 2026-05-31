---
title: Rewards & fulfillment
sidebar_position: 3
---

# Rewards & fulfillment

From winning a prize to delivering it — claim, the transactional outbox, the dispatcher, and the
signed n8n callback.

## Claim → outbox (on_claim mode)

```mermaid
sequenceDiagram
  autonumber
  participant W as Widget
  participant C as Consumer BFF
  participant Core as Core (RewardService)
  participant DB as SQL

  W->>C: POST /rewards/{id}/claim
  C->>Core: ClaimReward(scope, reward_id, player_id)
  Core->>DB: ownership check (reward.player_id == caller)
  rect rgb(237,233,254)
    Note over Core,DB: single transaction
    Core->>DB: reward won → claimed
    Core->>DB: enqueue fulfillment_task (status=pending)
  end
  Core--)Core: emit prize_claimed (best-effort)
  Core-->>C: { reward }
  C-->>W: 200 { reward: { status: "claimed" } }
```

For `instant` + in-app channels (`voucher_code`/none) the task is enqueued (or delivered) at **win**
time instead; the outbox row is always written in the same txn as the reward, so a delivery is never
lost or duplicated.

## Dispatcher drains the outbox

```mermaid
sequenceDiagram
  autonumber
  participant Disp as Dispatcher (worker)
  participant DB as SQL
  participant P as Provider (channel)
  loop every tick
    Disp->>DB: claim due tasks (FOR UPDATE SKIP LOCKED) → processing
    Disp->>P: Deliver(task)
    alt delivered (in-app)
      P-->>Disp: ok + receipt
      Disp->>DB: task fulfilled · reward fulfilled
    else retryable error
      P-->>Disp: err
      Disp->>DB: schedule retry (exponential backoff)
      Note over Disp,DB: after attempt budget → dead-letter
    else handed off (external_workflow)
      P-->>Disp: awaiting
      Disp->>DB: task stays processing (awaiting callback)
    end
  end
```

The dispatcher is multi-replica safe (`SKIP LOCKED`). Outcomes are also counted as the
`fulfillment_tasks_total{outcome}` metric (delivered/awaiting/retry/dead).

## n8n hand-off + signed callback

```mermaid
sequenceDiagram
  autonumber
  participant Disp as Dispatcher
  participant N as n8n
  participant A as Admin BFF
  participant DB as SQL

  Disp->>N: POST task payload (X-Muse-Signature = HMAC)
  Note over Disp,DB: task → processing (awaiting callback)
  N->>N: run no-code workflow (issue voucher, ship, …)
  N->>A: POST /api/v1/fulfillment/tasks/{id}/callback (signed) { status, receipt }
  A->>A: verify HMAC signature
  A->>Core: ReportResult(task_id, status, receipt)
  Core->>DB: task fulfilled/failed · store receipt · reward fulfilled
  A-->>N: 200
```

## Admin operations

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/admin/fulfillment/tasks` | list (filter status / campaign / prize) |
| `POST /api/v1/admin/fulfillment/tasks/{id}/retry` | re-arm a failed/dead task |
| `POST /api/v1/admin/rewards/{id}/fulfill` · `/revoke` | manual lifecycle control |

Proven by `make e2e-fulfillment` (outbox + dispatcher + dead-letter/retry + HMAC callback).
