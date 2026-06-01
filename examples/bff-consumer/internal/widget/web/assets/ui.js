// ui.js — shared presentation helpers used by all three game pages: a tiny DOM
// builder, a toast, a result modal, and reward formatting. Kept framework-free.

export function el(tag, attrs = {}, children = []) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") node.className = v;
    else if (k === "html") node.innerHTML = v;
    else if (k.startsWith("on") && typeof v === "function") {
      node.addEventListener(k.slice(2), v);
    } else if (v !== null && v !== undefined) {
      node.setAttribute(k, v);
    }
  }
  for (const c of [].concat(children)) {
    if (c == null) continue;
    node.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
  }
  return node;
}

let toastTimer = null;
export function toast(message, kind = "info") {
  let box = document.querySelector(".toast");
  if (!box) {
    box = el("div", { class: "toast" });
    document.body.appendChild(box);
  }
  box.className = `toast toast--${kind} toast--show`;
  box.textContent = message;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => box.classList.remove("toast--show"), 3200);
}

// imageOrEmoji renders an <img src=url> when url is set, falling back to the
// emoji if the URL is empty or the image fails to load. `cls` is applied either
// way; `em` sizes the image relative to the surrounding font-size box so it
// scales with its context (a 1.3rem icon vs a 2rem drop).
export function imageOrEmoji(url, emoji, cls, em = 1) {
  if (!url) return el("span", { class: cls }, emoji);
  const img = el("img", {
    class: cls,
    src: url,
    alt: "",
    loading: "lazy",
    style: `width:${em}em;height:${em}em;object-fit:contain;vertical-align:middle`,
  });
  img.addEventListener("error", () => img.replaceWith(el("span", { class: cls }, emoji)));
  return img;
}

// applyRenderConfig themes the current page from a game's opaque `ui` block —
// theme colors (as CSS custom properties overriding the stylesheet defaults)
// and an optional background image. Every field is optional; absent fields keep
// the built-in "Lì Xì" defaults. Returns the ui object so callers can read the
// game-specific bits (wheel.segments, items, egg).
export function applyRenderConfig(ui) {
  ui = ui || {};
  const root = document.documentElement;
  const theme = ui.theme || {};
  const map = { primary: "--red", accent: "--gold", ink: "--ink", paper: "--paper" };
  for (const [key, cssVar] of Object.entries(map)) {
    if (theme[key]) root.style.setProperty(cssVar, theme[key]);
  }
  if (ui.background_image) {
    // Darkening gradient keeps the foreground cards legible over any image.
    document.body.style.background =
      `linear-gradient(rgba(80,0,0,0.55), rgba(80,0,0,0.7)), url("${ui.background_image}") center/cover fixed`;
  }
  return ui;
}

// loadRenderConfig fetches a game's render config, applies the theme/background
// immediately, and resolves to the ui object (or {} on any error — theming is
// best-effort and must never block gameplay).
export async function loadRenderConfig(client, gameId) {
  try {
    const data = await client.render(gameId);
    return applyRenderConfig(data && data.ui);
  } catch {
    return applyRenderConfig({});
  }
}

// rewardItem renders one awarded prize as "🎁 Name ×2 (worth 100,000)", using
// the prize image when set (emoji fallback). Shared by the result modal and the
// history view.
export function rewardItem(r) {
  const qty = r.quantity && r.quantity > 1 ? ` ×${r.quantity}` : "";
  const worth = r.value ? ` · worth ${Number(r.value).toLocaleString()}` : "";
  const code = r.code ? ` · code ${r.code}` : "";
  return el("li", { class: "reward" }, [
    imageOrEmoji(r.image, "🎁", "reward__icon", 1.4),
    el("span", {}, `${r.name || r.prize_id || "Prize"}${qty}${worth}${code}`),
  ]);
}

// formatTimestamp renders a unix-seconds timestamp in the viewer's locale, or ""
// when unset.
export function formatTimestamp(unix) {
  if (!unix) return "";
  return new Date(Number(unix) * 1000).toLocaleString();
}

// showResult pops the win/no-win modal. `rewards` is the play response array;
// `detailLines` are game-specific strings (score, tier, slot, caught count).
export function showResult({ rewards = [], detailLines = [], onClose } = {}) {
  document.querySelector(".modal-backdrop")?.remove();
  const won = rewards.length > 0;
  const card = el("div", { class: `modal ${won ? "modal--win" : "modal--miss"}` }, [
    el("div", { class: "modal__emoji" }, won ? "🎉" : "🍀"),
    el("h2", { class: "modal__title" }, won ? "You won!" : "No prize this time"),
    detailLines.length
      ? el(
          "p",
          { class: "modal__detail" },
          detailLines.join(" · "),
        )
      : null,
    won
      ? el("ul", { class: "reward-list" }, rewards.map(rewardItem))
      : el("p", { class: "modal__detail" }, "Better luck on the next play."),
    el(
      "button",
      {
        class: "btn btn--primary",
        onclick: () => {
          backdrop.remove();
          onClose && onClose();
        },
      },
      "Play again",
    ),
  ]);
  const backdrop = el("div", { class: "modal-backdrop" }, [card]);
  backdrop.addEventListener("click", (e) => {
    if (e.target === backdrop) {
      backdrop.remove();
      onClose && onClose();
    }
  });
  document.body.appendChild(backdrop);
}

// renderEligibility paints the "N plays left" badge from an eligibility payload.
export function renderEligibility(node, elig) {
  if (!node) return;
  if (!elig) {
    node.textContent = "";
    return;
  }
  if (!elig.can_play) {
    node.className = "pill pill--blocked";
    node.textContent = elig.reason ? `Can't play: ${elig.reason}` : "No plays left";
    return;
  }
  node.className = "pill pill--ok";
  const n = elig.remaining_plays;
  node.textContent = n > 0 ? `${n} play${n === 1 ? "" : "s"} left` : "Ready";
}

// gameHeader builds the shared top bar (back link + title + eligibility pill).
// Pass { historyHref } to add a "History" link next to the back link.
export function gameHeader(title, subtitle, { historyHref } = {}) {
  const pill = el("span", { class: "pill" });
  const header = el("header", { class: "game-head" }, [
    el("a", { class: "back", href: "./index.html" }, "← Games"),
    el("div", { class: "game-head__titles" }, [
      el("h1", {}, title),
      subtitle ? el("p", { class: "muted" }, subtitle) : null,
    ]),
    historyHref ? el("a", { class: "back", href: historyHref }, "🕘 History") : null,
    pill,
  ]);
  return { header, pill };
}
