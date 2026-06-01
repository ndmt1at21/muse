// history.js — "Gift history": the player's past plays for one game and the
// prizes each play awarded. Reads GET /games/{gameId}/history/me and pages with
// the server cursor. Pure client of the public API, like the three game pages.
//
// The game id comes from ?game=… (the game pages link here with it set). Each
// entry shows when it was played, the rewards won (or "no prize"), and a compact
// line of the shape-specific metadata (score, tier, total_caught, …).

import { loadConfig, MuseClient, MuseError } from "./client.js";
import { el, toast, rewardItem, gameHeader, formatTimestamp } from "./ui.js";

const cfg = loadConfig();
const gameId = new URLSearchParams(location.search).get("game") || "";
const client = new MuseClient(cfg);

const { header } = gameHeader("Lịch Sử Quà", "Your past plays and the prizes you won");
document.getElementById("head").replaceChildren(header);
const root = document.getElementById("root");

if (!gameId) {
  root.replaceChildren(
    el("div", { class: "empty-note" }, [
      el("p", {}, "No game id — open history from a game page."),
      el("a", { class: "back", href: "./index.html" }, "← Back to games"),
    ]),
  );
  throw new Error("no game id");
}

const list = el("div", { class: "history-list" });
const moreBtn = el("button", { class: "btn btn--primary", onclick: () => load() }, "Load more");
const moreWrap = el("div", { class: "history-more", style: "display:none" }, [moreBtn]);
root.replaceChildren(el("div", { class: "panel history-panel" }, [list, moreWrap]));

let cursor = "";
let loading = false;
let rendered = 0;

// metaLine renders the flat (non-object) metadata as "score: 12 · tier: gold".
function metaLine(meta) {
  if (!meta || typeof meta !== "object") return null;
  const parts = Object.entries(meta)
    .filter(([, v]) => v !== null && v !== undefined && typeof v !== "object")
    .map(([k, v]) => `${k}: ${v}`);
  return parts.length ? parts.join(" · ") : null;
}

function renderEntry(e) {
  const rewards = Array.isArray(e.rewards) ? e.rewards : [];
  const won = rewards.length > 0;
  const ml = metaLine(e.metadata);
  return el("div", { class: `history-entry ${won ? "history-entry--win" : ""}` }, [
    el("div", { class: "history-entry__head" }, [
      el("span", { class: "history-entry__icon" }, won ? "🎁" : "🍀"),
      el("span", { class: "history-entry__time" }, formatTimestamp(e.created_at) || "—"),
    ]),
    won
      ? el("ul", { class: "reward-list" }, rewards.map(rewardItem))
      : el("p", { class: "muted history-entry__miss" }, "No prize this time."),
    ml ? el("p", { class: "history-entry__meta" }, ml) : null,
  ]);
}

async function load() {
  if (loading) return;
  loading = true;
  moreBtn.disabled = true;
  moreBtn.textContent = "Loading…";
  try {
    const data = await client.history(gameId, { cursor });
    const items = (data && data.items) || [];
    for (const e of items) {
      list.appendChild(renderEntry(e));
      rendered++;
    }
    const pg = (data && data.pagination) || {};
    cursor = pg.next_cursor || "";
    moreWrap.style.display = pg.has_more && cursor ? "" : "none";
    if (rendered === 0) {
      list.replaceChildren(
        el("div", { class: "empty-note" }, [
          el("p", {}, "No plays yet."),
          el("p", { class: "muted" }, "Play the game and your wins will show up here."),
        ]),
      );
    }
  } catch (e) {
    toast(e instanceof MuseError ? e.message : "Could not load history", "error");
  } finally {
    loading = false;
    moreBtn.disabled = false;
    moreBtn.textContent = "Load more";
  }
}

load();
