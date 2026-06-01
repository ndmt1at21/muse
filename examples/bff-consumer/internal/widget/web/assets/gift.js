// gift.js — Gift Catcher (shape: drop_sequence + collect_items + drop_plan).
//
// /start returns a server-authoritative drop sequence as seed_data:
//   { drops: [{drop_id, type, t}], plan: { type: {prize_id, max_catchable} },
//     interval_ms, total_items }
// We render each drop falling at its spawn offset `t`. Tapping one "catches" its
// drop_id. /play submits { caught_items: [...] }; collect_items awards one prize
// per caught unit (capped) and drop_plan rejects unknown ids, duplicates, or
// over-cap catches — so we mirror the per-type cap here to keep honest plays valid.

import { loadConfig, MuseClient, MuseError } from "./client.js";
import { el, toast, showResult, renderEligibility, gameHeader } from "./ui.js";

const cfg = loadConfig();
const gameId = new URLSearchParams(location.search).get("game") || cfg.games.gift;
const client = new MuseClient(cfg);

const { header, pill } = gameHeader("Hứng Quà", "Gift Catcher · tap falling gifts", {
  historyHref: gameId ? `./history.html?game=${encodeURIComponent(gameId)}` : null,
});
document.getElementById("head").replaceChildren(header);
const root = document.getElementById("root");

if (!gameId) {
  root.replaceChildren(
    el("div", { class: "empty-note" }, [
      el("p", {}, "No Gift Catcher game id configured."),
      el("p", { class: "muted", html: "Run <code>deploy/seed.sh</code> and open the link it prints, or set the id on the Games page." }),
      el("a", { class: "back", href: "./index.html" }, "← Back to games"),
    ]),
  );
  throw new Error("no game id");
}

const EMPTY = "empty";
const GIFT_EMOJIS = ["🎁", "🧧", "💝", "🎀", "🏆", "💎", "🪙"];
const FALL_MS = 2400;
const STAGE_H = 460;

function emojiForType(type) {
  if (type === EMPTY) return "🍂";
  let h = 0;
  for (const ch of type) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return GIFT_EMOJIS[h % GIFT_EMOJIS.length];
}

const caughtEl = el("span", { class: "big" }, "0");
const timeEl = el("span", { class: "big" }, "–");
const startBtn = el("button", { class: "btn btn--primary", onclick: startRound }, "Start");
const field = el("div", { class: "field-stage" });

root.replaceChildren(
  el("div", { class: "stage" }, [
    el("div", { class: "hud" }, [
      el("span", {}, ["⏱ ", timeEl, "s"]),
      el("span", {}, ["🎁 ", caughtEl]),
    ]),
    field,
    startBtn,
  ]),
);

let playing = false;
let plan = {};
const caught = []; // drop_ids submitted to the server
const caughtSet = new Set();
const perType = {}; // non-empty catches recorded, for client-side cap mirror
let active = []; // {id, type, node, y, speed}
let rafId = 0;
let lastFrame = 0;
let pendingTimers = [];
let roundEnd = 0;
let endTimer = null;

function reset() {
  field.replaceChildren();
  caught.length = 0;
  caughtSet.clear();
  for (const k of Object.keys(perType)) delete perType[k];
  active = [];
  pendingTimers.forEach(clearTimeout);
  pendingTimers = [];
  caughtEl.textContent = "0";
}

function spawnDrop(drop) {
  if (!playing) return;
  const stageW = field.clientWidth || 380;
  const node = el("div", { class: "drop" }, emojiForType(drop.type));
  const x = 12 + Math.random() * (stageW - 48);
  node.style.left = x + "px";
  node.style.top = "-40px";
  const d = { id: drop.drop_id, type: drop.type, node, y: -40, speed: (STAGE_H + 60) / FALL_MS };
  node.addEventListener("click", () => catchDrop(d));
  node.addEventListener("touchstart", (e) => { e.preventDefault(); catchDrop(d); }, { passive: false });
  field.appendChild(node);
  active.push(d);
}

function removeDrop(d, caughtClass) {
  if (caughtClass) d.node.classList.add("drop--caught");
  const i = active.indexOf(d);
  if (i >= 0) active.splice(i, 1);
  setTimeout(() => d.node.remove(), caughtClass ? 180 : 0);
}

function catchDrop(d) {
  if (!playing || caughtSet.has(d.id)) return;
  if (d.type !== EMPTY) {
    const cap = (plan[d.type] && plan[d.type].max_catchable) || 0;
    const have = perType[d.type] || 0;
    if (cap > 0 && have >= cap) {
      // already at the legitimate cap — pop it but don't submit (server would reject an over-claim)
      toast(`Max ${cap} of this gift`, "info");
      removeDrop(d, true);
      return;
    }
    perType[d.type] = have + 1;
  }
  caughtSet.add(d.id);
  caught.push(d.id);
  caughtEl.textContent = String(caught.length);
  removeDrop(d, true);
}

function frame(now) {
  const dt = now - lastFrame;
  lastFrame = now;
  for (const d of active.slice()) {
    d.y += d.speed * dt;
    d.node.style.top = d.y + "px";
    if (d.y > STAGE_H + 20) removeDrop(d, false); // missed
  }
  timeEl.textContent = Math.max(0, (roundEnd - now) / 1000).toFixed(0);
  if (playing) rafId = requestAnimationFrame(frame);
}

async function startRound() {
  if (playing) return;
  startBtn.disabled = true;
  let session;
  try {
    session = await client.start(gameId);
  } catch (e) {
    toast(e instanceof MuseError ? e.message : "Could not start", "error");
    startBtn.disabled = false;
    await refreshEligibility();
    return;
  }

  const seed = session.seed_data || {};
  const drops = Array.isArray(seed.drops) ? seed.drops : [];
  plan = seed.plan || {};
  if (!drops.length) {
    toast("Server sent no drops for this game", "error");
    startBtn.disabled = false;
    return;
  }

  reset();
  playing = true;
  pill.className = "pill pill--ok";
  pill.textContent = "Playing…";

  // Schedule each drop at its server-issued spawn offset t (ms from start).
  const maxT = drops.reduce((m, d) => Math.max(m, d.t || 0), 0);
  for (const drop of drops) {
    pendingTimers.push(setTimeout(() => spawnDrop(drop), drop.t || 0));
  }

  const total = maxT + FALL_MS + 200;
  lastFrame = performance.now();
  roundEnd = lastFrame + total;
  rafId = requestAnimationFrame(frame);
  endTimer = setTimeout(() => endRound(session), total);
}

async function endRound(session) {
  playing = false;
  cancelAnimationFrame(rafId);
  clearTimeout(endTimer);
  pendingTimers.forEach(clearTimeout);
  active.forEach((d) => d.node.remove());
  active = [];
  timeEl.textContent = "0";

  try {
    const result = await client.play(gameId, session.session_id, { caught_items: caught });
    const meta = result.metadata || {};
    const lines = [`Caught ${meta.total_caught ?? caught.length}`];
    if (meta.by_type && Object.keys(meta.by_type).length) {
      lines.push(
        Object.entries(meta.by_type)
          .map(([t, n]) => `${n}× ${t}`)
          .join(", "),
      );
    }
    showResult({ rewards: result.rewards || [], detailLines: lines, onClose: refreshEligibility });
  } catch (e) {
    const msg = e instanceof MuseError ? e.message : "Submit failed";
    toast(msg, "error");
  } finally {
    startBtn.disabled = false;
    startBtn.textContent = "Play again";
    await refreshEligibility();
  }
}

async function refreshEligibility() {
  if (playing) return;
  try {
    const elig = await client.eligibility(gameId);
    renderEligibility(pill, elig);
    startBtn.disabled = !elig.can_play;
  } catch {
    renderEligibility(pill, null);
  }
}

field.style.height = STAGE_H + "px";
refreshEligibility();
