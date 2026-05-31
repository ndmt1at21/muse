---
title: Leaderboard
sidebar_position: 5
---

# Leaderboard: update → read → finalize

Every play folds into the active boards of its campaign; players read live ranks off Redis; an admin
finalizes to lock and batch-award prize tiers.

## Update (post-Play hook)

```mermaid
sequenceDiagram
  autonumber
  participant Core as Core (engine)
  participant LB as LeaderboardHook
  participant DB as SQL
  participant R as Redis (ZSET)

  Note over Core: Play already committed
  Core->>LB: OnPlay(scope, game, player, rewards, metadata)
  LB->>DB: upsert durable entry (score/plays)
  LB->>R: ZADD live sorted set
  LB-->>Core: may annotate metadata.rankings
  Note over Core,LB: best-effort — a failure never fails the play
```

## Read (real-time)

```mermaid
sequenceDiagram
  autonumber
  participant W as Widget
  participant C as Consumer BFF
  participant Core as Core (LeaderboardService)
  participant R as Redis (ZSET)

  W->>C: GET /leaderboards/{id}/rankings?limit=20
  Note over C: served from the read-model cache (short TTL)
  C->>Core: GetRankings (on cache miss)
  Core->>R: ZREVRANGE top-N
  Core-->>C: ranked entries
  C-->>W: 200 top-N

  W->>C: GET /leaderboards/{id}/my-rank   (player JWT)
  C->>Core: MyRank
  Core->>R: ZREVRANK player
  Core-->>C: { rank, ranks_to_next_tier }
```

## Finalize (admin)

```mermaid
sequenceDiagram
  autonumber
  participant A as Admin BFF
  participant Core as Core
  participant R as Redis
  participant DB as SQL

  A->>Core: Finalize(scope, leaderboard_id)
  Core->>R: acquire finalize lock
  Core->>DB: snapshot durable entries (skip flagged)
  Core->>DB: batch-award prize tiers → reward records (+ fulfillment)
  Core--)Core: emit leaderboard_finalized
  Core-->>A: { awarded, awards[] }
```

Anti-cheat: outlier / velocity checks move suspicious entries to `flagged`; admins can
`disqualify` / `adjust`; **finalize only awards un-flagged ranks**.
