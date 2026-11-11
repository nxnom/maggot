// Small DOM helpers. Everything from tools or runs is inserted as text, never HTML.

const PROPS = new Set(["value", "checked", "disabled", "open", "selected", "hidden", "multiple", "indeterminate"]);

export function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v === null || v === undefined || v === false) continue;
      if (k === "class") el.className = v;
      else if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2), v);
      else if (PROPS.has(k)) el[k] = v;
      else el.setAttribute(k, v === true ? "" : String(v));
    }
  }
  append(el, kids);
  return el;
}

export function append(el, kids) {
  for (const k of kids.flat(Infinity)) {
    if (k === null || k === undefined || k === false) continue;
    el.append(k instanceof Node ? k : document.createTextNode(String(k)));
  }
  return el;
}

export function clear(el, ...kids) {
  el.replaceChildren();
  return append(el, kids);
}

export function toast(message, isError = false) {
  const box = document.getElementById("toasts");
  const t = h("div", { class: "toast" + (isError ? " err" : "") }, message);
  box.append(t);
  setTimeout(() => t.remove(), isError ? 7000 : 3500);
}

// openDialog shows a modal. actions: [{label, class, onClick}] — onClick may
// return false (or a promise of false) to keep the dialog open.
export function openDialog({ title, body, actions = [], onClose }) {
  const foot = h("div", { class: "dlg-foot" });
  const dlg = h("dialog", { "aria-label": title },
    h("div", { class: "dlg-head" }, h("h2", null, title)),
    h("div", { class: "dlg-body" }, body),
    foot);
  const close = () => { if (dlg.open) dlg.close(); };
  for (const a of actions) {
    const b = h("button", { class: "btn " + (a.class || ""), type: "button" }, a.label);
    b.addEventListener("click", async () => {
      b.disabled = true;
      try {
        const keep = a.onClick ? await a.onClick() : undefined;
        if (keep !== false) close();
      } finally { b.disabled = false; }
    });
    foot.append(b);
  }
  dlg.addEventListener("close", () => { dlg.remove(); onClose && onClose(); });
  document.body.append(dlg);
  dlg.showModal();
  return { dlg, close, foot };
}

export function confirmDialog(title, message, okLabel = "OK", danger = false) {
  return new Promise((resolve) => {
    let result = false;
    openDialog({
      title,
      body: typeof message === "string" ? h("p", null, message) : message,
      actions: [
        { label: "Cancel", class: "quiet" },
        { label: okLabel, class: danger ? "danger" : "green", onClick: () => { result = true; } },
      ],
      onClose: () => resolve(result),
    });
  });
}

export function bytes(n) {
  if (n < 1024) return n + " B";
  const units = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
  return n.toFixed(n < 10 ? 1 : 0) + " " + units[i];
}

export function ago(iso) {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + " min ago";
  if (s < 86400) return Math.floor(s / 3600) + " h ago";
  return new Date(iso).toLocaleDateString();
}

export function duration(startIso, endIso) {
  const s = Math.max(0, ((endIso ? new Date(endIso) : new Date()) - new Date(startIso)) / 1000);
  if (s < 60) return s.toFixed(s < 10 ? 1 : 0) + " s";
  const m = Math.floor(s / 60);
  return m + " min " + Math.round(s % 60) + " s";
}

export function store(key, value) {
  try {
    if (value === undefined) return JSON.parse(localStorage.getItem("maggot:" + key) || "null");
    localStorage.setItem("maggot:" + key, JSON.stringify(value));
  } catch { return null; }
}

export function debounce(fn, ms) {
  let t;
  return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
}
