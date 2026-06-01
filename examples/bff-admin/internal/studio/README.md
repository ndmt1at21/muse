# Game Studio (demo)

A buildless config-and-play page embedded into the `bff-admin` binary
(`//go:embed web`), served at **`http://localhost:8081/studio/`**. It turns the
seed script into a browser UI: tune a game's prizes and odds, click **Apply** to
create it, and play it inline on the same screen.

It lives on the **admin** BFF on purpose — creating/configuring games is an
internal operation. The page makes two kinds of calls:

- **Config → admin BFF** (same origin, `:8081`): `POST /api/v1/admin/campaigns`,
  `/admin/prizes`, `/admin/games`, carrying the `X-Roles: admin` dev header.
- **Gameplay → consumer BFF** (cross-origin, `:8080`): `POST /games/{id}/start`
  + `/play`. The consumer's widget CORS (`Access-Control-Allow-Origin: *`)
  permits this, so the same page both configures and plays.

Each **Apply** creates a fresh game (there is no `UpdateGame` RPC), so the game
id changes on every apply — the panel shows the new id and an **Open in player**
link to the polished animated widget (`:8080/play/...`).

## Panels

| Panel | Tunable knobs | Inline play |
|-------|---------------|-------------|
| 🎡 Spin Wheel | prize value/stock, jackpot/small/no-win weights, max plays | spin wheel + "Play 20×" odds distribution |
| 🥚 Egg Catcher | prize value/stock, score→tier thresholds, max-score guard, max plays | score slider → submit → tier + reward |
| 🎁 Gift Catcher | prize value/stock, per-type drop frequency & catch cap, total items, max plays | choose how many of each to catch → reward |

The **Consumer URL** field (top bar) defaults to the studio's host with port
`8080`; change it if your consumer BFF is elsewhere. Tenant/merchant/player are
sent as dev headers.

> Dev/demo only: the page sends `X-Roles: admin` from the browser. In production
> the admin BFF sits behind real auth and would not serve a public tuning page.
