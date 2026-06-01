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

// rewardItem renders one awarded prize as "🎁 Name ×2 (worth 100,000)".
// Shared by the result modal and the history view.
export function rewardItem(r) {
  const qty = r.quantity && r.quantity > 1 ? ` ×${r.quantity}` : "";
  const worth = r.value ? ` · worth ${Number(r.value).toLocaleString()}` : "";
  const code = r.code ? ` · code ${r.code}` : "";
  return el("li", { class: "reward" }, [
    el("span", { class: "reward__icon" }, r.image ? "" : "🎁"),
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
