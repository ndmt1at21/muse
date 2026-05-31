# Muse documentation site

A [Docusaurus](https://docusaurus.io/) site that visualizes Muse's architecture and flows
(Mermaid diagrams) and documents how to run and extend it.

## Develop

```bash
cd website
npm install
npm start          # live dev server at http://localhost:3000
```

## Build

```bash
npm run build      # static site → website/build
npm run serve      # preview the production build
```

From the repo root you can also use `make docs` (dev server) and `make docs-build` (static build).

## Structure

- `docs/intro.md` — landing page (mounted at `/`).
- `docs/architecture/` — components, runtime topology, tenancy/identity, data model.
- `docs/concepts/` — the generic engine, anti-cheat, rewards/fulfillment, wallet, quests/leaderboard, integration hub.
- `docs/flows/` — Mermaid **sequence diagrams** for gameplay, auth, fulfillment, wallet, leaderboard, integration events.
- `docs/guides/` — quickstart, add-a-game (config only), add-a-shape (one handler).
- `docs/reference/` — REST API, error reference, observability.

Diagrams are fenced ` ```mermaid ` blocks (enabled via `@docusaurus/theme-mermaid`). Navigation is
defined in `sidebars.js`.
