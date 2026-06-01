# Demo game widget

A buildless, dependency-free game widget embedded into the `bff-consumer`
binary (`//go:embed web`). It is the **presentation layer** the architecture
keeps out of Core: the pages are plain clients of the public `/api/v1` gameplay
endpoints (`/start`, `/play`, `/eligibility`, `/history/me`, `/render`) — no
privileged access, no build step, no framework.

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

## Theming (per-game render config)

Each game carries an opaque `ui` JSON block. Core stores and returns it but
**never interprets** it — the widget decodes it to theme itself per game, so an
admin can restyle a game any time (via `PUT /admin/games/{id}`) with no rebuild.
The widget fetches it from the **redacted** `GET /games/{gameId}/render`
endpoint, which returns only `{game_id, name, type, ui}` — never the odds in
`handler_config`. Every field is optional; absent fields keep the built-in "Lì
Xì" defaults, and a broken image URL falls back to its emoji.

```jsonc
{
  "background_image": "https://cdn.example/bg.jpg",        // page background (optional)
  "theme": { "primary": "#c1121f", "accent": "#f4c430",    // CSS-var overrides
             "ink": "#2b1216", "paper": "#fff8f0" },
  "wheel": { "segments": [                                 // spin: decorative ring (NOT the prize layout)
    { "emoji": "💎", "color": "#1f6feb" },
    { "image": "https://cdn.example/seg.png", "color": "#f4c430" }
  ] },
  "items": {                                               // gift: by drop type
    "gift_big":   { "image": "https://cdn.example/big.png", "emoji": "💎" },
    "gift_small": { "emoji": "🎀" }
  },
  "egg": { "emoji": "🟡", "image": "https://cdn.example/egg.png" }  // egg-catcher
}
```

Anti-cheat note: the spin wheel is decorative — `wheel.segments` are not tied to
prizes (the BFF never exposes the prize table or odds), so configuring segment
images can't leak which slot wins. `deploy/games.json` seeds an example `ui` per
game (emoji + theme colors; image URLs work the same way).

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
