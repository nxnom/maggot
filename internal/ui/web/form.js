// Builds a form from a tool's `inputs`. Each widget has get() → value or
// undefined (not sent), and set(value).
import { h, toast } from "./dom.js";
import { upload } from "./api.js";
import { pick } from "./picker.js";

const titleOf = (inp) => inp.label || inp.name.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());

export function buildForm(inputs, initial, onChange) {
  const widgets = new Map();
  const changed = () => onChange && onChange();

  // Main inputs first, then each group in order of first appearance.
  const groups = new Map([["", []]]);
  for (const inp of inputs) {
    const g = inp.group || "";
    if (!groups.has(g)) groups.set(g, []);
    groups.get(g).push(inp);
  }

  const el = h("div", { class: "form-body" });
  for (const [name, list] of groups) {
    if (!list.length) continue;
    const box = h("div", { class: "fieldset" });
    if (name) box.append(h("h3", { class: "group-title" }, name));
    const plain = list.filter((i) => !i.advanced);
    const adv = list.filter((i) => i.advanced);
    for (const inp of plain) box.append(field(inp));
    if (adv.length) {
      const d = h("details", { class: "advanced" }, h("summary", null, name ? `More ${name.toLowerCase()} options` : "Advanced options"));
      for (const inp of adv) d.append(field(inp));
      box.append(d);
    }
    el.append(box);
  }
  if (!inputs.length) {
    el.append(h("p", { class: "muted", style: "margin:0 0 14px" },
      "This tool does not describe its inputs, so it runs with no arguments here. Use the terminal for other arguments."));
  }

  function field(inp) {
    const id = "in-" + inp.name;
    const w = widget(inp, id, changed);
    widgets.set(inp.name, w);
    const start = initial && inp.name in initial ? initial[inp.name] : inp.default;
    if (start !== undefined) w.set(start);
    const help = inp.description ? h("div", { class: "help", id: id + "-help" }, inp.description) : null;
    if (inp.type === "boolean") {
      return h("div", { class: "field" }, w.el, help);
    }
    const labelEl = w.group
      ? h("div", { class: "label", id: id + "-label" }, titleOf(inp), inp.required ? h("span", { class: "req-mark" }, "required") : null)
      : h("label", { for: id }, titleOf(inp), inp.required ? h("span", { class: "req-mark" }, "required") : null);
    return h("div", { class: "field" }, labelEl, w.el, help);
  }

  return {
    el,
    values() {
      const out = {};
      for (const [name, w] of widgets) {
        const v = w.get();
        if (v !== undefined) out[name] = v;
      }
      return out;
    },
    set(values) {
      for (const [name, w] of widgets) {
        const inp = inputs.find((i) => i.name === name);
        w.set(values && name in values ? values[name] : inp.default);
      }
      changed();
    },
  };
}

function widget(inp, id, changed) {
  const described = inp.description ? id + "-help" : null;
  switch (inp.type) {
    case "boolean": {
      const box = h("input", { type: "checkbox", id, "aria-describedby": described, onchange: changed });
      return {
        el: h("label", { class: "check", for: id }, box, h("span", { class: "label" }, titleOf(inp))),
        get: () => box.checked,
        set: (v) => { box.checked = v === true; },
      };
    }
    case "integer":
    case "number": {
      const box = h("input", {
        class: "input", type: "number", id, inputmode: inp.type === "integer" ? "numeric" : "decimal",
        step: inp.type === "integer" ? "1" : "any", min: inp.min, max: inp.max,
        placeholder: inp.placeholder, "aria-describedby": described, oninput: changed,
      });
      return {
        el: box,
        get: () => (box.value.trim() === "" ? undefined : Number(box.value)),
        set: (v) => { box.value = v === undefined || v === null ? "" : String(v); },
      };
    }
    case "enum":
      return enumWidget(inp, id, described, changed);
    case "text":
    case "strings": {
      const box = h("textarea", {
        class: "textarea", id, placeholder: inp.placeholder || (inp.type === "strings" ? "One per line" : null),
        "aria-describedby": described, oninput: changed, spellcheck: inp.type === "text" ? null : "false",
      });
      return {
        el: box,
        get: () => {
          if (inp.type === "text") return box.value === "" ? undefined : box.value;
          const lines = box.value.split("\n").map((s) => s.trim()).filter(Boolean);
          return lines.length ? lines : undefined;
        },
        set: (v) => { box.value = Array.isArray(v) ? v.join("\n") : v ?? ""; },
      };
    }
    case "file":
    case "directory":
      return pathWidget(inp, id, described, changed);
    case "files":
      return filesWidget(inp, id, changed);
    default: { // string, args
      const box = h("input", {
        class: "input" + (inp.type === "args" ? " mono" : ""), id, type: "text",
        placeholder: inp.placeholder, "aria-describedby": described, oninput: changed,
        spellcheck: inp.type === "args" ? "false" : null, autocomplete: "off",
      });
      return {
        el: box,
        get: () => (box.value.trim() === "" ? undefined : box.value),
        set: (v) => { box.value = v ?? ""; },
      };
    }
  }
}

function optLabel(o) { return o.label || o.value; }

function enumWidget(inp, id, described, changed) {
  const opts = inp.options || [];
  const short = opts.length <= 4 && opts.every((o) => optLabel(o).length <= 22);
  const hasDefault = inp.default !== undefined;
  if (short && (inp.required || hasDefault)) {
    const name = id + "-r";
    const radios = opts.map((o) => h("input", { type: "radio", name, value: o.value, onchange: changed }));
    const el = h("div", { class: "segmented", role: "radiogroup", "aria-labelledby": id + "-label", "aria-describedby": described },
      opts.map((o, i) => h("label", null, radios[i], h("span", null, optLabel(o)))));
    return {
      el, group: true,
      get: () => { const r = radios.find((x) => x.checked); return r ? r.value : undefined; },
      set: (v) => { for (const r of radios) r.checked = r.value === v; },
    };
  }
  const sel = h("select", { class: "select", id, "aria-describedby": described, onchange: changed },
    inp.required && hasDefault ? null : h("option", { value: "" }, hasDefault ? "(default)" : "Not set"),
    opts.map((o) => h("option", { value: o.value }, optLabel(o))));
  return {
    el: sel,
    get: () => (sel.value === "" ? undefined : sel.value),
    set: (v) => { sel.value = v ?? ""; },
  };
}

// Dropping files on a widget uploads them to the maggot data folder and uses
// the uploaded paths.
function dropTarget(el, onPaths) {
  el.addEventListener("dragover", (e) => {
    if (!e.dataTransfer.types.includes("Files")) return;
    e.preventDefault(); e.stopPropagation(); el.classList.add("over");
  });
  el.addEventListener("dragleave", () => el.classList.remove("over"));
  el.addEventListener("drop", async (e) => {
    if (!e.dataTransfer.files.length) return;
    e.preventDefault(); e.stopPropagation(); el.classList.remove("over");
    try {
      onPaths(await upload(e.dataTransfer.files));
    } catch (err) { toast(err.message, true); }
  });
}

function pathWidget(inp, id, described, changed) {
  const box = h("input", { class: "input mono", id, placeholder: inp.placeholder || (inp.type === "file" ? "/path/to/file" : "/path/to/folder"), "aria-describedby": described, oninput: changed, spellcheck: "false" });
  const browse = h("button", { class: "btn small", type: "button" }, "Browse…");
  browse.addEventListener("click", async () => {
    const p = await pick({ mode: inp.type === "file" ? "file" : "dir", start: box.value || undefined, accept: inp.accept });
    if (p) { box.value = p; changed(); }
  });
  const el = h("div", { class: "drop" }, h("div", { class: "row" }, box, browse),
    inp.type === "file" ? h("div", { class: "hint" }, "Or drop a file here.") : null);
  if (inp.type === "file") dropTarget(el, (paths) => { box.value = paths[0]; changed(); });
  return {
    el,
    get: () => (box.value.trim() === "" ? undefined : box.value.trim()),
    set: (v) => { box.value = v ?? ""; },
  };
}

function filesWidget(inp, id, changed) {
  let paths = [];
  const rows = h("ul", { class: "file-rows", id });
  const render = () => {
    rows.replaceChildren(...paths.map((p, i) => h("li", null,
      h("span", { class: "path mono", title: p }, p),
      h("button", { class: "btn small quiet", type: "button", "aria-label": "Remove " + p, onclick: () => { paths.splice(i, 1); render(); changed(); } }, "Remove"))));
  };
  const add = (list) => { for (const p of list) if (!paths.includes(p)) paths.push(p); render(); changed(); };
  const browse = h("button", { class: "btn small", type: "button" }, "Add files…");
  browse.addEventListener("click", async () => {
    const chosen = await pick({ mode: "files", accept: inp.accept });
    if (chosen) add(chosen);
  });
  const typed = h("input", { class: "input mono", placeholder: "Or type a path and press Enter", "aria-label": "Add a path", spellcheck: "false" });
  typed.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && typed.value.trim()) { e.preventDefault(); add([typed.value.trim()]); typed.value = ""; }
  });
  const el = h("div", { class: "drop" }, rows, h("div", { class: "row" }, browse, typed),
    h("div", { class: "hint" }, "Or drop files here."));
  dropTarget(el, add);
  return {
    el, group: true,
    get: () => (paths.length ? [...paths] : undefined),
    set: (v) => { paths = Array.isArray(v) ? [...v] : []; render(); },
  };
}
