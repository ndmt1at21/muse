# Demo game widget

A buildless, dependency-free game widget embedded into the `bff-consumer`
binary (`//go:embed web`). It is the **presentation layer** the architecture
keeps out of Core: the pages are plain clients of the public `/api/v1` gameplay
endpoints (`/start`, `/play`, `/eligibility`, `/history/me`) — no privileged
access, no build step, no framework.

## Run it

```bash
make up            # Core + both BFFs + datastores (docker compose)
deploy/seed.sh     # creates a campaign, prizes, and the 3 demo games
```

`seed.sh` prints a ready-to-play URL, e.g.

```
http://localhost:8080/play/?tenant=tenant_demo&merchant=merchant_demo&player=demo_player&spin=<id>&egg=<id>&gift=<id>
```

## Tuning the games

The three games are defined in [`deploy/games.json`](../../../../deploy/games.json) — prizes,
spin probabilities, egg score tiers, gift drop frequencies/caps, stock, and per-user
play limits. Edit a value (e.g. raise the spin jackpot `probability`) and re-run
`make seed` (or `deploy/seed.sh`); the seed resolves prize references and recreates
the games with the new config, then prints a fresh URL. No code changes, no rebuild.

Opening `http://localhost:8080/` redirects to the lobby (`/play/`). The lobby
lets you edit the scope (tenant/merchant/player) and the three game ids; values
are persisted to `localStorage`, and URL query params override + prefill them.
Scope is sent as the `X-Tenant-Id` / `X-Merchant-Id` / `X-Player-Id` dev headers.

## The three games (one per built-in shape)

| Page        | Game            | Shape (`seed` + `handler` + `validator`)            | Client input |
|-------------|-----------------|-----------------------------------------------------|--------------|
| `spin.html` | Vòng Quay May Mắn | `none` + `probability` + `basic`                   | none — server draws |
| `egg.html`  | Đập Trứng Vàng  | `none` + `score_to_tier` + `time_and_score_range`   | `{score, duration_ms}` |
| `gift.html` | Hứng Quà        | `drop_sequence` + `collect_items` + `drop_plan`     | `{caught_items:[drop_id…]}` |

The server is authoritative in all three: the spin wheel never sees the prize
table or odds (the wheel is decorative; the modal shows the real result), the
egg game's score is validated against a ceiling and a minimum duration, and the
gift game's catches are checked against the server-issued drop sequence. The
gift UI mirrors each type's `max_catchable` cap client-side so an honest play
never trips the `drop_plan` anti-cheat.

## History

Every game page has a **🕘 History** link in its header that opens
`history.html?game=<id>`. The history view lists the caller's past plays for
that game — newest first, with the prizes each play awarded and a compact line
of the play's metadata (slot, score+tier, caught counts) — by paging
`GET /games/{gameId}/history/me` with its opaque cursor (a **Load more** button
appends the next page). The player is the same one the lobby resolves
(`?player=` / `localStorage`), sent as the `X-Player-Id` dev header, so a player
only ever sees their own history.

## Files

```
widget.go            embeds web/ and mounts it at /play (root "/" redirects there)
widget_test.go       httptest coverage of routing + JS MIME types
web/
  index.html         lobby + demo settings
  spin.html / egg.html / gift.html
  history.html       per-game play history (linked from each game header)
  assets/
    client.js        config resolver + MuseClient (envelope unwrap, dev headers)
    ui.js            DOM helper, toast, result modal, reward + timestamp formatters
    styles.css       shared "Lì Xì" theme
    spin.js / egg.js / gift.js
    history.js       renders /history/me with cursor pagination
```

Adding a visual is a widget change only — never a Core change.
