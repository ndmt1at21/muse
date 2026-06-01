// egg.js — Egg Catcher (shape: none + score_to_tier + time_and_score_range).
//
// A timed whack-the-egg skill game. The only client input that matters is the
// final { score, duration_ms } — the server maps the score to a prize tier and
// draws within it, and the time_and_score_range validator rejects impossible
// scores or implausibly short plays. The eggs are just how the player earns a
// score honestly.

import { loadConfig, MuseClient, MuseError } from "./client.js";
import { el, toast, showResult, renderEligibility, gameHeader } from "./ui.js";

const cfg = loadConfig();
const gameId = new URLSearchParams(location.search).get("game") || cfg.games.egg;
const client = new MuseClient(cfg);

const { header, pill } = gameHeader("Đập Trứng Vàng", "Egg Catcher · smash eggs, earn a tier", {
  historyHref: gameId ? `./history.html?game=${encodeURIComponent(gameId)}` : null,
});
document.getElementById("head").replaceChildren(header);
const root = document.getElementById("root");

if (!gameId) {
  root.replaceChildren(
    el("div", { class: "empty-note" }, [
      el("p", {}, "No Egg Catcher game id configured."),
      el("p", { class: "muted", html: "Run <code>deploy/seed.sh</code> and open the link it prints, or set the id on the Games page." }),
      el("a", { class: "back", href: "./index.html" }, "← Back to games"),
    ]),
  );
  throw new Error("no game id");
}

const ROUND_MS = 15000;
const CELLS = 8;
const SPAWN_EVERY = 520;
const EGG_LIFE = 1100;

let score = 0;
let playing = false;
let roundStart = 0;
let spawnTimer = null;
let endTimer = null;
const cellTimers = new Map();

const scoreEl = el("span", { class: "big" }, "0");
const timeEl = el("span", { class: "big" }, "15");
const startBtn = el("button", { class: "btn btn--primary", onclick: startRound }, "Start");

const cells = Array.from({ length: CELLS }, (_, i) =>
  el("button", { class: "egg", "data-i": i, disabled: "", onclick: () => whack(i) }, ""),
);
const grid = el("div", { class: "egg-grid" }, cells);

root.replaceChildren(
  el("div", { class: "stage" }, [
    el("div", { class: "hud" }, [
      el("span", {}, ["⏱ ", timeEl, "s"]),
      el("span", {}, ["⭐ ", scoreEl]),
    ]),
    grid,
    startBtn,
  ]),
);

function clearCell(i) {
  const c = cells[i];
  c.textContent = "";
  c.classList.remove("egg--live", "egg--cracked");
  const t = cellTimers.get(i);
  if (t) {
    clearTimeout(t);
    cellTimers.delete(i);
  }
}

function spawn() {
  const empty = cells.map((_, i) => i).filter((i) => !cells[i].classList.contains("egg--live"));
  if (!empty.length) return;
  const i = empty[Math.floor(Math.random() * empty.length)];
  const c = cells[i];
  c.classList.add("egg--live");
  c.textContent = "🥚";
  cellTimers.set(i, setTimeout(() => clearCell(i), EGG_LIFE));
}

function whack(i) {
  if (!playing) return;
  const c = cells[i];
  if (!c.classList.contains("egg--live")) return;
  score++;
  scoreEl.textContent = String(score);
  c.classList.remove("egg--live");
  c.classList.add("egg--cracked");
  c.textContent = "🐣";
  const t = cellTimers.get(i);
  if (t) clearTimeout(t);
  cellTimers.set(i, setTimeout(() => clearCell(i), 220));
}

function tickTimer() {
  const left = Math.max(0, ROUND_MS - (performance.now() - roundStart));
  timeEl.textContent = (left / 1000).toFixed(0);
  if (left > 0 && playing) requestAnimationFrame(tickTimer);
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

  // reset board
  score = 0;
  scoreEl.textContent = "0";
  cells.forEach((_, i) => clearCell(i));
  cells.forEach((c) => (c.disabled = false));
  playing = true;
  roundStart = performance.now();
  pill.className = "pill pill--ok";
  pill.textContent = "Playing…";

  tickTimer();
  spawnTimer = setInterval(spawn, SPAWN_EVERY);
  endTimer = setTimeout(() => endRound(session), ROUND_MS);
}

async function endRound(session) {
  playing = false;
  clearInterval(spawnTimer);
  clearTimeout(endTimer);
  cellTimers.forEach((t) => clearTimeout(t));
  cellTimers.clear();
  cells.forEach((c, i) => {
    c.disabled = true;
    clearCell(i);
  });
  const durationMs = Math.round(performance.now() - roundStart);

  try {
    const result = await client.play(gameId, session.session_id, {
      score,
      duration_ms: durationMs,
    });
    const meta = result.metadata || {};
    const lines = [`Score ${meta.score ?? score}`];
    if (meta.tier) lines.push(`Tier ${meta.tier}`);
    showResult({ rewards: result.rewards || [], detailLines: lines, onClose: refreshEligibility });
  } catch (e) {
    toast(e instanceof MuseError ? e.message : "Submit failed", "error");
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

refreshEligibility();
