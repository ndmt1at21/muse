---
title: Wallet redeem
sidebar_position: 4
---

# Wallet: earn → milestone → redeem

A collection game credits a wallet currency on every play; once the balance reaches a milestone
threshold, the player redeems it for a real prize.

## Earning (inside Play)

```mermaid
sequenceDiagram
  autonumber
  participant W as Widget
  participant Core as Core (engine + WalletRouter)
  participant DB as SQL

  W->>Core: Play (lucky_item game)
  Core->>Core: lucky_item handler → reward { type: lucky_item, name: "star" }
  rect rgb(237,233,254)
    Note over Core,DB: inside the Play transaction
    Core->>DB: credit wallet ledger (scope_key, "star", +1)
    Core->>DB: read new balance
    alt cumulative_unlock + auto_grant and threshold crossed
      Core->>DB: grant milestone prize once (mint reward)
    end
    Core->>DB: insert play_history
  end
  Core-->>W: rewards (incl. any auto-granted milestone prize)
```

## Redeeming (manual claim or spend-exchange)

```mermaid
sequenceDiagram
  autonumber
  participant W as Widget
  participant C as Consumer BFF
  participant Core as Core (WalletService)
  participant DB as SQL

  W->>C: GET /games/{id}/milestones   (player JWT)
  C->>Core: GetMilestones(scope, game, player)
  Core-->>C: { currency, mode, balance, milestones[] (status/progress/remaining) }
  C-->>W: 200 progress view

  W->>C: POST /games/{id}/redeem { milestone_id }
  C->>Core: Redeem(scope, game, milestone, player)
  rect rgb(237,233,254)
    Note over Core,DB: single transaction
    alt spend_exchange
      Core->>DB: atomic spend threshold (fail if insufficient)
    else cumulative_unlock
      Core->>DB: check balance ≥ threshold
      Core->>DB: grant-once guard (2nd redeem → ALREADY_EXISTS)
    end
    Core->>DB: mint milestone prize → reward (+ fulfillment task if async)
  end
  Core-->>C: { redeemed, mode, spent, reward, balances }
  C-->>W: 200 { reward }
```

## Reading the wallet

```mermaid
sequenceDiagram
  participant W as Widget
  participant C as Consumer BFF
  participant Core as Core
  W->>C: GET /wallet?scope_key=…        (balances)
  W->>C: GET /wallet/ledger?scope_key=… (movements, newest-first)
  C->>Core: GetWallet / GetLedger
  Core-->>C: balances / ledger entries
```

Proven by `make e2e-wallet`: two plays accrue 2 `lucky_star`, the milestone shows `unlocked`,
`redeem` grants the prize, and a second `redeem` returns `ALREADY_EXISTS`.
