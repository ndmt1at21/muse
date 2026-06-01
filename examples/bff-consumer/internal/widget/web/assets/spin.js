// spin.js — Spin Wheel (shape: none + probability + basic).
//
// Flow: POST /start → POST /play (empty payload). The server is authoritative:
// it returns the won reward (if any) plus metadata.slot_index. The wheel is
// decorative — the consumer BFF never exposes the prize table or odds — so we
// land the pointer on a segment derived from slot_index and show the real,
// server-decided result in the modal.

import { loadConfig, MuseClient, MuseError } from "./client.js";
import { el, toast, showResult, renderEligibility, gameHeader, loadRenderConfig } from "./ui.js";

const cfg = loadConfig();
const gameId = new URLSearchParams(location.search).get("game") || cfg.games.spin;
const client = new MuseClient(cfg);

const { header, pill } = gameHeader("Vòng Quay May Mắn", "Spin Wheel · tap to spin", {
  historyHref: gameId ? `./history.html?game=${encodeURIComponent(gameId)}` : null,
});
document.getElementById("head").replaceChildren(header);
const root = document.getElementById("root");

if (!gameId) {
  root.replaceChildren(
    el("div", { class: "empty-note" }, [
      el("p", {}, "No Spin Wheel game id configured."),
      el("p", { class: "muted", html: "Run <code>deploy/seed.sh</code> and open the link it prints, or set the id on the Games page." }),
      el("a", { class: "back", href: "./index.html" }, "← Back to games"),
    ]),
  );
  throw new Error("no game id");
}

// ---- wheel rendering ----
// The wheel is purely decorative — segments are NOT the prize layout (the BFF
// never exposes odds/prizes), so per-segment images/emoji/colors from the game's
// render config are safe to show. Defaults are the built-in Tết emoji ring.
const COLORS = ["#c1121f", "#f4c430"];
const TEXT_COLORS = ["#fff8f0", "#780000"];
const TWO_PI = Math.PI * 2;
const DEFAULT_EMOJIS = ["🧧", "🎁", "💰", "🍀", "⭐", "🎊", "🪙", "🎈"];
let segments = DEFAULT_EMOJIS.map((emoji, i) => ({ emoji, color: COLORS[i % COLORS.length] }));
let N = segments.length;
let SEG = TWO_PI / N;

const size = 320;
const dpr = Math.min(window.devicePixelRatio || 1, 2);
const canvas = el("canvas", { width: size * dpr, height: size * dpr });
canvas.style.width = size + "px";
canvas.style.height = size + "px";
const ctx = canvas.getContext("2d");
ctx.scale(dpr, dpr);
const cx = size / 2;
const cy = size / 2;
const R = size / 2 - 8;

let rotation = -Math.PI / 2; // start with segment 0 under the top pointer
let spinning = false;

function draw() {
  ctx.clearRect(0, 0, size, size);
  ctx.save();
  ctx.translate(cx, cy);
  ctx.rotate(rotation);
  for (let i = 0; i < N; i++) {
    const seg = segments[i];
    const start = i * SEG;
    ctx.beginPath();
    ctx.moveTo(0, 0);
    ctx.arc(0, 0, R, start, start + SEG);
    ctx.closePath();
    ctx.fillStyle = seg.color || COLORS[i % COLORS.length];
    ctx.fill();
    ctx.save();
    ctx.rotate(start + SEG / 2);
    ctx.translate(R * 0.66, 0);
    ctx.rotate(Math.PI / 2);
    const img = seg._img;
    if (img && img.complete && img.naturalWidth) {
      ctx.drawImage(img, -20, -20, 40, 40);
    } else {
      ctx.fillStyle = TEXT_COLORS[i % TEXT_COLORS.length];
      ctx.font = "26px serif";
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      ctx.fillText(seg.emoji || "🎁", 0, 0);
    }
    ctx.restore();
  }
  ctx.restore();

  // hub
  ctx.beginPath();
  ctx.arc(cx, cy, 26, 0, TWO_PI);
  ctx.fillStyle = "#fff8f0";
  ctx.fill();
  ctx.lineWidth = 4;
  ctx.strokeStyle = "#f4c430";
  ctx.stroke();
  ctx.fillStyle = "#c1121f";
  ctx.font = "20px serif";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.fillText("🎯", cx, cy);

  // pointer (fixed, at top)
  ctx.beginPath();
  ctx.moveTo(cx - 14, 2);
  ctx.lineTo(cx + 14, 2);
  ctx.lineTo(cx, 30);
  ctx.closePath();
  ctx.fillStyle = "#780000";
  ctx.fill();
}

const easeOutCubic = (t) => 1 - Math.pow(1 - t, 3);

function spinTo(segment, durationMs) {
  return new Promise((resolve) => {
    const center = segment * SEG + SEG / 2;
    const desired = ((Math.PI * 1.5 - center) % TWO_PI + TWO_PI) % TWO_PI;
    const current = ((rotation % TWO_PI) + TWO_PI) % TWO_PI;
    let delta = (desired - current + TWO_PI) % TWO_PI;
    const from = rotation;
    const to = from + 5 * TWO_PI + delta; // 5 full turns + settle
    const t0 = performance.now();
    function frame(now) {
      const t = Math.min(1, (now - t0) / durationMs);
      rotation = from + (to - from) * easeOutCubic(t);
      draw();
      if (t < 1) requestAnimationFrame(frame);
      else resolve();
    }
    requestAnimationFrame(frame);
  });
}

// ---- controls ----
const spinBtn = el("button", { class: "btn btn--primary", onclick: spin }, "SPIN");
root.replaceChildren(
  el("div", { class: "stage" }, [
    canvas,
    el("div", { class: "hud" }, [spinBtn]),
  ]),
);
draw();

// Apply per-game render config: theme/background, plus decorative wheel segments
// (emoji/image/color). Redraws once images load. Best-effort — never blocks play.
loadRenderConfig(client, gameId).then((ui) => {
  const segs = ui.wheel && Array.isArray(ui.wheel.segments) ? ui.wheel.segments : null;
  if (segs && segs.length) {
    segments = segs.map((s, i) => ({
      emoji: s.emoji || "🎁",
      color: s.color || COLORS[i % COLORS.length],
    }));
    N = segments.length;
    SEG = TWO_PI / N;
    segs.forEach((s, i) => {
      if (s.image) {
        const im = new Image();
        im.onload = draw;
        im.src = s.image;
        segments[i]._img = im;
      }
    });
  }
  draw();
});

async function refreshEligibility() {
  try {
    const elig = await client.eligibility(gameId);
    renderEligibility(pill, elig);
    spinBtn.disabled = !elig.can_play;
  } catch (e) {
    renderEligibility(pill, null);
  }
}

async function spin() {
  if (spinning) return;
  spinning = true;
  spinBtn.disabled = true;
  try {
    const session = await client.start(gameId);
    const result = await client.play(gameId, session.session_id, {});
    const slot = result.metadata && typeof result.metadata.slot_index === "number"
      ? result.metadata.slot_index
      : Math.floor(Math.random() * N);
    await spinTo(((slot % N) + N) % N, 3600);
    const rewards = result.rewards || [];
    showResult({
      rewards,
      detailLines: [`Slot ${slot}`],
      onClose: refreshEligibility,
    });
  } catch (e) {
    const msg = e instanceof MuseError ? e.message : "Something went wrong";
    toast(msg, "error");
    if (e instanceof MuseError && (e.reason === "PLAYS_EXHAUSTED" || e.httpStatus === 429)) {
      await refreshEligibility();
    }
  } finally {
    spinning = false;
    await refreshEligibility();
  }
}

refreshEligibility();
