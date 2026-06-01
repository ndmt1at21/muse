// client.js — the demo widget's tiny runtime: a config resolver and a thin
// wrapper over the public /api/v1 gameplay endpoints. No framework, no build.
//
// Scope (tenant/merchant/player) and the three game ids are resolved once, in
// this order: URL query (?tenant=…&spin=…) → localStorage → defaults. seed.sh
// prints a URL that prefills everything; the lobby persists edits.

const STORE_KEY = "muse.demo.config";

function randomPlayer() {
  return "player_" + Math.random().toString(36).slice(2, 10);
}

export function loadConfig() {
  let saved = {};
  try {
    saved = JSON.parse(localStorage.getItem(STORE_KEY) || "{}");
  } catch {
    saved = {};
  }
  const q = new URLSearchParams(location.search);
  const savedGames = saved.games || {};
  const cfg = {
    apiBase: q.get("api") || saved.apiBase || "/api/v1",
    tenant: q.get("tenant") || saved.tenant || "tenant_demo",
    merchant: q.get("merchant") || saved.merchant || "merchant_demo",
    player: q.get("player") || saved.player || randomPlayer(),
    games: {
      spin: q.get("spin") || savedGames.spin || "",
      egg: q.get("egg") || savedGames.egg || "",
      gift: q.get("gift") || savedGames.gift || "",
    },
  };
  // Persist whatever we resolved so deep-linking once is enough.
  saveConfig(cfg);
  return cfg;
}

export function saveConfig(cfg) {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify(cfg));
  } catch {
    /* private mode / disabled storage — config just won't persist */
  }
}

// MuseError carries the envelope so callers can branch on the canonical code
// (e.g. RESOURCE_EXHAUSTED → out of plays, FAILED_PRECONDITION → not eligible).
export class MuseError extends Error {
  constructor(message, envelope, httpStatus) {
    super(message || "request failed");
    this.name = "MuseError";
    this.envelope = envelope || {};
    this.reason = (envelope && envelope.error && envelope.error.reason) || "";
    this.httpStatus = httpStatus;
  }
}

export class MuseClient {
  constructor(cfg) {
    this.cfg = cfg;
  }

  headers() {
    return {
      "Content-Type": "application/json",
      "X-Tenant-Id": this.cfg.tenant,
      "X-Merchant-Id": this.cfg.merchant,
      "X-Player-Id": this.cfg.player,
    };
  }

  async req(method, path, body) {
    let res;
    try {
      res = await fetch(this.cfg.apiBase + path, {
        method,
        headers: this.headers(),
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (networkErr) {
      throw new MuseError("Cannot reach the API — is bff-consumer running?", {}, 0);
    }
    let env = {};
    try {
      env = await res.json();
    } catch {
      env = {};
    }
    // Success envelope is { code: 0, message: "ok", data }. Anything else is an
    // error mapped from a gRPC status.
    if (!res.ok || (env && typeof env.code === "number" && env.code !== 0)) {
      throw new MuseError(env.message, env, res.status);
    }
    return env.data;
  }

  eligibility(gameId) {
    return this.req("GET", `/games/${encodeURIComponent(gameId)}/eligibility`);
  }
  start(gameId) {
    return this.req("POST", `/games/${encodeURIComponent(gameId)}/start`, {});
  }
  play(gameId, sessionId, payload) {
    return this.req("POST", `/games/${encodeURIComponent(gameId)}/play`, {
      session_id: sessionId,
      payload: payload || {},
    });
  }
  history(gameId, { cursor, limit } = {}) {
    const q = new URLSearchParams();
    if (cursor) q.set("cursor", cursor);
    if (limit) q.set("limit", String(limit));
    const qs = q.toString();
    return this.req(
      "GET",
      `/games/${encodeURIComponent(gameId)}/history/me${qs ? `?${qs}` : ""}`,
    );
  }
}
