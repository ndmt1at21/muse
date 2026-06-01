// studio.js — the demo Game Studio runtime. Builds three config panels
// (spin / egg / gift), creates games via the admin BFF (same origin, carrying
// the X-Roles:admin dev header), and plays them inline against the consumer BFF
// (cross-origin, allowed by the consumer's widget CORS). No framework, no build.

// ---------- tiny DOM helper ----------
function el(tag, attrs = {}, children = []) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") n.className = v;
    else if (k === "html") n.innerHTML = v;
    else if (k.startsWith("on") && typeof v === "function") n.addEventListener(k.slice(2), v);
    else if (v !== null && v !== undefined && v !== false) n.setAttribute(k, v);
  }
  for (const c of [].concat(children)) {
    if (c == null) continue;
    n.appendChild(typeof c === "string" || typeof c === "number" ? document.createTextNode(String(c)) : c);
  }
  return n;
}
const num = (v, d) => (Number.isFinite(parseFloat(v)) ? parseFloat(v) : d);

// ---------- settings ----------
const SKEY = "muse.studio.settings";
function loadSettings() {
  let s = {};
  try { s = JSON.parse(localStorage.getItem(SKEY) || "{}"); } catch { s = {}; }
  return {
    tenant: s.tenant || "tenant_demo",
    merchant: s.merchant || "merchant_demo",
    player: s.player || "studio_player",
    // studio runs on the admin BFF; gameplay hits the consumer BFF (default :8080).
    consumerBase: s.consumerBase || location.origin.replace(/:\d+$/, ":8080"),
  };
}
function saveSettings(s) {
  try { localStorage.setItem(SKEY, JSON.stringify(s)); } catch { /* ignore */ }
}
let settings = loadSettings();

// ---------- API clients (envelope: { code:0, message:"ok", data }) ----------
async function call(base, headers, method, path, body) {
  let res;
  try {
    res = await fetch(base + "/api/v1" + path, {
      method, headers, body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new Error("network error — is the service running?");
  }
  let env = {};
  try { env = await res.json(); } catch { env = {}; }
  if (!res.ok || (env && typeof env.code === "number" && env.code !== 0)) {
    throw new Error((env && env.message) || `HTTP ${res.status}`);
  }
  return env.data;
}
const adminHeaders = () => ({
  "Content-Type": "application/json",
  "X-Tenant-Id": settings.tenant,
  "X-Merchant-Id": settings.merchant,
  "X-Roles": "admin",
});
const playerHeaders = () => ({
  "Content-Type": "application/json",
  "X-Tenant-Id": settings.tenant,
  "X-Merchant-Id": settings.merchant,
  "X-Player-Id": settings.player,
});
const admin = {
  campaign: (name) => call(location.origin, adminHeaders(), "POST", "/admin/campaigns",
    { name, status: "CAMPAIGN_STATUS_ACTIVE" }).then((d) => d.campaign_id),
  prize: (name, type, value, stock) => call(location.origin, adminHeaders(), "POST", "/admin/prizes",
    { name, type, value, stock: { total: stock } }).then((d) => d.prize_id),
  game: (body) => call(location.origin, adminHeaders(), "POST", "/admin/games", body).then((d) => d.game_id),
};
const player = {
  start: (id) => call(settings.consumerBase, playerHeaders(), "POST", `/games/${id}/start`, {}),
  play: (id, sid, payload) => call(settings.consumerBase, playerHeaders(), "POST", `/games/${id}/play`,
    { session_id: sid, payload }),
};

let campaignId = null;
async function ensureCampaign() {
  if (!campaignId) campaignId = await admin.campaign("Studio Demo");
  return campaignId;
}

// ---------- settings bar ----------
function renderSettings() {
  const box = document.getElementById("settings");
  const field = (key, label) =>
    el("div", { class: "fld" }, [
      el("label", {}, label),
      el("input", {
        value: settings[key],
        oninput: (e) => { settings[key] = e.target.value.trim(); saveSettings(settings); },
      }),
    ]);
  box.replaceChildren(
    field("tenant", "Tenant id"),
    field("merchant", "Merchant id"),
    field("player", "Player id"),
    field("consumerBase", "Consumer URL (gameplay)"),
  );
}

// ---------- form builder ----------
function buildForm(groups, state) {
  const frag = document.createDocumentFragment();
  for (const g of groups) {
    const fields = g.fields.map((f) =>
      el("div", { class: "fld" }, [
        el("label", { for: f.key }, f.label),
        el("input", {
          id: f.key, type: "number", step: f.step ?? "1", min: f.min ?? "0",
          value: state[f.key],
          oninput: (e) => { state[f.key] = num(e.target.value, state[f.key]); },
        }),
      ]),
    );
    frag.appendChild(el("fieldset", {}, [el("legend", {}, g.legend), el("div", { class: "grid2" }, fields)]));
  }
  return frag;
}

// ---------- spin wheel (compact, decorative) ----------
function makeWheel(mount) {
  const SEG = ["🧧", "🎁", "💰", "🍀", "⭐", "🎊", "🪙", "🎈"], N = 8, TAU = Math.PI * 2;
  const size = 240, dpr = Math.min(devicePixelRatio || 1, 2);
  const c = el("canvas", { width: size * dpr, height: size * dpr });
  c.style.width = c.style.height = size + "px";
  const x = c.getContext("2d"); x.scale(dpr, dpr);
  const R = size / 2 - 6, cx = size / 2;
  let rot = -Math.PI / 2;
  function draw() {
    x.clearRect(0, 0, size, size);
    x.save(); x.translate(cx, cx); x.rotate(rot);
    for (let i = 0; i < N; i++) {
      x.beginPath(); x.moveTo(0, 0); x.arc(0, 0, R, i * TAU / N, (i + 1) * TAU / N); x.closePath();
      x.fillStyle = i % 2 ? "#f4c430" : "#c1121f"; x.fill();
      x.save(); x.rotate(i * TAU / N + Math.PI / N); x.translate(R * 0.64, 0); x.rotate(Math.PI / 2);
      x.font = "20px serif"; x.textAlign = "center"; x.textBaseline = "middle"; x.fillText(SEG[i], 0, 0); x.restore();
    }
    x.restore();
    x.beginPath(); x.moveTo(cx - 11, 1); x.lineTo(cx + 11, 1); x.lineTo(cx, 24); x.closePath();
    x.fillStyle = "#780000"; x.fill();
  }
  draw();
  mount.appendChild(c);
  return (slot) => new Promise((resolve) => {
    const center = (slot % N) * TAU / N + TAU / N / 2;
    const desired = ((Math.PI * 1.5 - center) % TAU + TAU) % TAU;
    const cur = ((rot % TAU) + TAU) % TAU;
    const to = rot + 5 * TAU + ((desired - cur + TAU) % TAU);
    const from = rot, t0 = performance.now(), dur = 2800;
    (function frame(now) {
      const t = Math.min(1, (now - t0) / dur);
      rot = from + (to - from) * (1 - Math.pow(1 - t, 3));
      draw();
      t < 1 ? requestAnimationFrame(frame) : resolve();
    })(performance.now());
  });
}

function distBars(mount, tally) {
  const total = Object.values(tally).reduce((a, b) => a + b, 0) || 1;
  mount.replaceChildren(
    ...Object.entries(tally).sort((a, b) => b[1] - a[1]).map(([name, n]) =>
      el("div", { class: "bar" }, [
        el("span", {}, name),
        el("div", { class: "track" }, [el("div", { class: "fill", style: `width:${(n / total) * 100}%` })]),
        el("span", {}, `${n}`),
      ]),
    ),
  );
}

function playerLink(page, gameId) {
  const q = new URLSearchParams({
    game: gameId, tenant: settings.tenant, merchant: settings.merchant, player: settings.player,
  });
  return el("a", { class: "link", href: `${settings.consumerBase}/play/${page}?${q}`, target: "_blank" },
    "Open in player ↗");
}

// ---------- panel factory ----------
function panel({ emoji, title, shape, groups, state, apply, mountPlay }) {
  const status = el("div", { class: "status" });
  const playBox = el("div", { class: "play", hidden: true });
  const links = el("div", {});
  const applyBtn = el("button", { class: "btn btn--primary" }, "Apply & create");

  applyBtn.addEventListener("click", async () => {
    applyBtn.disabled = true;
    status.className = "status"; status.textContent = "Creating…";
    try {
      const gameId = await apply(state);
      status.className = "status status--ok";
      status.textContent = `Created ${gameId}`;
      links.replaceChildren(playerLink(state._page, gameId));
      playBox.hidden = false;
      playBox.replaceChildren();
      mountPlay(playBox, gameId, state);
    } catch (e) {
      status.className = "status status--err";
      status.textContent = e.message || "failed";
    } finally {
      applyBtn.disabled = false;
    }
  });

  return el("section", { class: "panel" }, [
    el("h2", {}, [el("span", {}, emoji), el("span", {}, title)]),
    el("div", { class: "shape" }, shape),
    buildForm(groups, state),
    el("div", { class: "row" }, [applyBtn, links]),
    status,
    playBox,
  ]);
}

// ---------- SPIN ----------
function spinPanel() {
  const state = {
    _page: "spin.html",
    jackpotValue: 100000, jackpotStock: 50, smallValue: 10000, smallStock: 500,
    wJackpot: 0.05, wSmall: 0.25, wMiss: 0.7, maxPlays: 100,
  };
  return panel({
    emoji: "🎡", title: "Spin Wheel", shape: "none + probability + basic",
    state,
    groups: [
      { legend: "Prizes", fields: [
        { key: "jackpotValue", label: "Jackpot value" }, { key: "jackpotStock", label: "Jackpot stock" },
        { key: "smallValue", label: "Small value" }, { key: "smallStock", label: "Small stock" },
      ] },
      { legend: "Odds (weights, auto-normalised)", fields: [
        { key: "wJackpot", label: "Jackpot", step: "0.01" }, { key: "wSmall", label: "Small", step: "0.01" },
        { key: "wMiss", label: "No win", step: "0.01" }, { key: "maxPlays", label: "Max plays/user" },
      ] },
    ],
    apply: async (s) => {
      await ensureCampaign();
      const jp = await admin.prize("Voucher (jackpot)", "voucher", s.jackpotValue, s.jackpotStock);
      const sm = await admin.prize("Voucher (small)", "voucher", s.smallValue, s.smallStock);
      return admin.game({
        name: "Vòng Quay May Mắn", type: "spin_wheel", campaign_id: campaignId,
        seed_generator: "none", reward_handler: "probability", validator: "basic",
        status: "GAME_STATUS_ACTIVE", rules: { max_plays_per_user: s.maxPlays },
        handler_config: { prizes: [
          { prize_id: jp, probability: s.wJackpot, slot_index: 0 },
          { prize_id: sm, probability: s.wSmall, slot_index: 3 },
          { prize_id: "", probability: s.wMiss, slot_index: 6 },
        ] },
      });
    },
    mountPlay: (box, gameId) => {
      const wheelBox = el("div", {});
      const spinTo = makeWheel(wheelBox);
      const result = el("div", { class: "result" });
      const dist = el("div", { class: "dist" });
      let busy = false;
      const spinOnce = async () => {
        const ses = await player.start(gameId);
        const r = await player.play(gameId, ses.session_id, {});
        const slot = (r.metadata && r.metadata.slot_index) | 0;
        await spinTo(slot);
        const w = (r.rewards || [])[0];
        result.innerHTML = w ? `🎉 You won <b>${w.name}</b>` : "🍀 No win";
      };
      const oneBtn = el("button", { class: "btn btn--primary", onclick: async () => {
        if (busy) return; busy = true; oneBtn.disabled = batchBtn.disabled = true;
        try { await spinOnce(); } catch (e) { result.textContent = e.message; }
        busy = false; oneBtn.disabled = batchBtn.disabled = false;
      } }, "Spin");
      const batchBtn = el("button", { class: "btn btn--ghost", onclick: async () => {
        if (busy) return; busy = true; oneBtn.disabled = batchBtn.disabled = true;
        const tally = {};
        for (let i = 0; i < 20; i++) {
          try {
            const ses = await player.start(gameId);
            const r = await player.play(gameId, ses.session_id, {});
            const name = (r.rewards || [])[0]?.name || "No win";
            tally[name] = (tally[name] || 0) + 1;
          } catch (e) { result.textContent = e.message; break; }
        }
        distBars(dist, tally);
        result.textContent = "20 plays:";
        busy = false; oneBtn.disabled = batchBtn.disabled = false;
      } }, "Play 20× (distribution)");
      box.append(wheelBox, el("div", { class: "row" }, [oneBtn, batchBtn]), result, dist);
    },
  });
}

// ---------- EGG ----------
function eggPanel() {
  const state = {
    _page: "egg.html",
    jackpotValue: 100000, jackpotStock: 50, smallValue: 10000, smallStock: 500,
    smallFrom: 8, jackpotFrom: 18, maxScore: 80, maxPlays: 100,
  };
  return panel({
    emoji: "🥚", title: "Egg Catcher", shape: "none + score_to_tier + time_and_score_range",
    state,
    groups: [
      { legend: "Prizes", fields: [
        { key: "jackpotValue", label: "Jackpot value" }, { key: "jackpotStock", label: "Jackpot stock" },
        { key: "smallValue", label: "Small value" }, { key: "smallStock", label: "Small stock" },
      ] },
      { legend: "Score tiers", fields: [
        { key: "smallFrom", label: "Small from score ≥" }, { key: "jackpotFrom", label: "Jackpot from score ≥" },
        { key: "maxScore", label: "Max score (anti-cheat)" }, { key: "maxPlays", label: "Max plays/user" },
      ] },
    ],
    apply: async (s) => {
      await ensureCampaign();
      const jp = await admin.prize("Voucher (jackpot)", "voucher", s.jackpotValue, s.jackpotStock);
      const sm = await admin.prize("Voucher (small)", "voucher", s.smallValue, s.smallStock);
      return admin.game({
        name: "Đập Trứng Vàng", type: "egg_catcher", campaign_id: campaignId,
        seed_generator: "none", reward_handler: "score_to_tier", validator: "time_and_score_range",
        status: "GAME_STATUS_ACTIVE", rules: { max_plays_per_user: s.maxPlays },
        handler_config: {
          tiers: [
            { min: 0, max: s.smallFrom - 1, prize_group: "t0" },
            { min: s.smallFrom, max: s.jackpotFrom - 1, prize_group: "t1" },
            { min: s.jackpotFrom, max: 100000, prize_group: "t2" },
          ],
          prize_groups: { t1: [{ prize_id: sm, probability: 1 }], t2: [{ prize_id: jp, probability: 1 }] },
        },
        validator_config: { min_duration_ms: 2000, max_duration_ms: 60000, max_score: s.maxScore },
      });
    },
    mountPlay: (box, gameId, s) => {
      const val = el("b", {}, "0");
      const slider = el("input", { type: "range", min: "0", max: String(s.maxScore), value: "0", style: "width:220px",
        oninput: (e) => (val.textContent = e.target.value) });
      const result = el("div", { class: "result" });
      const go = el("button", { class: "btn btn--primary", onclick: async () => {
        go.disabled = true;
        try {
          const ses = await player.start(gameId);
          const r = await player.play(gameId, ses.session_id, { score: +slider.value, duration_ms: 15000 });
          const w = (r.rewards || [])[0];
          const tier = r.metadata && r.metadata.tier;
          result.innerHTML = `score ${slider.value} → tier <b>${tier || "—"}</b> · ` +
            (w ? `won <b>${w.name}</b>` : "no prize");
        } catch (e) { result.textContent = e.message; }
        go.disabled = false;
      } }, "Submit score");
      box.append(el("div", { class: "row" }, ["Score: ", val, slider]), go, result);
    },
  });
}

// ---------- GIFT ----------
function giftPanel() {
  const state = {
    _page: "gift.html",
    bigValue: 100000, bigStock: 50, smallValue: 10000, smallStock: 500,
    bigFreq: 2, bigCap: 1, smallFreq: 6, smallCap: 3, totalItems: 22, intervalMs: 450, maxPlays: 100,
  };
  return panel({
    emoji: "🎁", title: "Gift Catcher", shape: "drop_sequence + collect_items + drop_plan",
    state,
    groups: [
      { legend: "Prizes", fields: [
        { key: "bigValue", label: "Big value" }, { key: "bigStock", label: "Big stock" },
        { key: "smallValue", label: "Small value" }, { key: "smallStock", label: "Small stock" },
      ] },
      { legend: "Drops (frequency = how many fall; cap = max catchable)", fields: [
        { key: "bigFreq", label: "Big frequency" }, { key: "bigCap", label: "Big cap" },
        { key: "smallFreq", label: "Small frequency" }, { key: "smallCap", label: "Small cap" },
        { key: "totalItems", label: "Total items" }, { key: "maxPlays", label: "Max plays/user" },
      ] },
    ],
    apply: async (s) => {
      await ensureCampaign();
      const big = await admin.prize("Voucher (big)", "voucher", s.bigValue, s.bigStock);
      const sm = await admin.prize("Voucher (small)", "voucher", s.smallValue, s.smallStock);
      return admin.game({
        name: "Hứng Quà", type: "gift_catcher", campaign_id: campaignId,
        seed_generator: "drop_sequence", reward_handler: "collect_items", validator: "drop_plan",
        status: "GAME_STATUS_ACTIVE", rules: { max_plays_per_user: s.maxPlays },
        handler_config: {
          drops: [
            { type: "gift_big", prize_id: big, frequency: s.bigFreq, max_catchable: s.bigCap },
            { type: "gift_small", prize_id: sm, frequency: s.smallFreq, max_catchable: s.smallCap },
          ],
          total_items: s.totalItems, interval_ms: s.intervalMs,
        },
      });
    },
    mountPlay: (box, gameId, s) => {
      const catchBig = el("input", { type: "number", min: "0", max: String(s.bigCap), value: String(s.bigCap), style: "width:60px" });
      const catchSmall = el("input", { type: "number", min: "0", max: String(s.smallCap), value: String(s.smallCap), style: "width:60px" });
      const result = el("div", { class: "result" });
      const go = el("button", { class: "btn btn--primary", onclick: async () => {
        go.disabled = true;
        try {
          const ses = await player.start(gameId);
          const drops = (ses.seed_data && ses.seed_data.drops) || [];
          // pick drop_ids per type, clamped to the catch caps (server rejects over-cap)
          const want = { gift_big: Math.min(+catchBig.value, s.bigCap), gift_small: Math.min(+catchSmall.value, s.smallCap) };
          const got = { gift_big: 0, gift_small: 0 }, caught = [];
          for (const d of drops) {
            if (want[d.type] && got[d.type] < want[d.type]) { caught.push(d.drop_id); got[d.type]++; }
          }
          const r = await player.play(gameId, ses.session_id, { caught_items: caught });
          const by = (r.metadata && r.metadata.by_type) || {};
          const names = (r.rewards || []).map((x) => x.name);
          result.innerHTML = `caught ${caught.length} · won <b>${names.length ? names.join(", ") : "nothing"}</b>` +
            (Object.keys(by).length ? ` <span class="muted">(${Object.entries(by).map(([t, n]) => `${n}×${t}`).join(", ")})</span>` : "");
        } catch (e) { result.textContent = e.message; }
        go.disabled = false;
      } }, "Play");
      box.append(
        el("div", { class: "row" }, ["catch big ", catchBig, " small ", catchSmall]),
        el("div", { class: "muted" }, "Sequence is server-generated; catches are capped to avoid the anti-cheat."),
        go, result,
      );
    },
  });
}

// ---------- mount ----------
renderSettings();
document.getElementById("panels").replaceChildren(spinPanel(), eggPanel(), giftPanel());
