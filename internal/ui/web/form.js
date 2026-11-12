// Builds a form from a tool's `inputs`. Each widget has get() → value or
// undefined (not sent), and set(value).
import { h, clear, toast, debounce } from "./dom.js";
import { upload } from "./api.js";
import { pick } from "./picker.js";
import { localKind, localFile, mediaURL, openLocalViewer } from "./viewer.js";

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

// Files must match the input's accept list (".json", "video/*"), like
// the picker shows. File objects carry a type; paths are typed by extension.
function matches(accept, name, type) {
  if (!accept || !accept.length) return true;
  const lower = name.toLowerCase();
  const mime = (type || localFile(name).mime).toLowerCase();
  return accept.some((a) => {
    a = a.trim().toLowerCase();
    if (!a) return false;
    if (a.startsWith(".")) return lower.endsWith(a);
    if (a.endsWith("/*")) return mime.startsWith(a.slice(0, -1));
    return mime === a;
  });
}

const KIND_WORDS = { "image/*": "images", "video/*": "videos", "audio/*": "audio files" };
const acceptText = (accept) => accept.map((a) => KIND_WORDS[a] || a).join(", ");

// keep returns the items that match; it tells the user about the rest.
function keep(inp, items, nameOf, typeOf) {
  const ok = items.filter((x) => matches(inp.accept, nameOf(x), typeOf(x)));
  if (ok.length < items.length) {
    const bad = items.filter((x) => !ok.includes(x)).map(nameOf).join(", ");
    toast(`${titleOf(inp)} takes only ${acceptText(inp.accept)}, so ${bad} was not used.`, true);
  }
  return ok;
}
const keepFiles = (inp, files) => keep(inp, files, (f) => f.name || "pasted", (f) => f.type);
const keepPaths = (inp, paths) => keep(inp, paths, (p) => p, () => "");

// Dropping files on a widget uploads them to the maggot data folder and uses
// the uploaded paths.
function dropTarget(el, inp, onPaths) {
  el.addEventListener("dragover", (e) => {
    if (!e.dataTransfer.types.includes("Files")) return;
    e.preventDefault(); e.stopPropagation(); el.classList.add("over");
  });
  el.addEventListener("dragleave", () => el.classList.remove("over"));
  el.addEventListener("drop", async (e) => {
    if (!e.dataTransfer.files.length) return;
    e.preventDefault(); e.stopPropagation(); el.classList.remove("over");
    const files = keepFiles(inp, [...e.dataTransfer.files]);
    if (!files.length) return;
    try {
      onPaths(await upload(files));
    } catch (err) { toast(err.message, true); }
  });
}

const PASTE_KEY = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent) ? "⌘V" : "Ctrl+V";

// A copied screenshot arrives as "image.png"; give it a name that says
// where it came from and does not clash.
function pastedName(f) {
  if (f.name && !/^image\.\w+$/i.test(f.name)) return f;
  const ext = ((f.type.split("/")[1] || "png").replace("jpeg", "jpg").replace(/\+.*/, ""));
  const d = new Date();
  const pad = (n) => String(n).padStart(2, "0");
  const stamp = `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
  return new File([f], `pasted-${stamp}.${ext}`, { type: f.type });
}

// Text that looks like file paths (one per line), for pasting a copied path.
function textPaths(text) {
  const lines = (text || "").split(/\r?\n/).map((l) => l.trim().replace(/^file:\/\//, "")).filter(Boolean);
  return lines.length && lines.every((l) => l.startsWith("/") || l.startsWith("~")) ? lines.map(decodeURI) : [];
}

// pasteTarget makes ⌘V inside el (on the box or any field in it) use a
// copied image or copied files: they are uploaded like dropped files.
function pasteTarget(el, inp, onPaths) {
  el.addEventListener("paste", async (e) => {
    let files = [...(e.clipboardData?.files || [])];
    if (!files.length) return; // plain text: let the field take it
    e.preventDefault();
    files = keepFiles(inp, files.map(pastedName));
    if (!files.length) return;
    try { onPaths(await upload(files)); } catch (err) { toast(err.message, true); }
  });
}

// pasteButton reads the clipboard on click: images are uploaded, copied
// paths are used as they are.
function pasteButton(inp, onPaths) {
  return h("button", {
    class: "btn small", type: "button", title: `Paste a copied image or path (or click the box and press ${PASTE_KEY})`,
    onclick: async () => {
      if (!navigator.clipboard?.read) return toast(`Click the box and press ${PASTE_KEY} to paste.`, true);
      try {
        const items = await navigator.clipboard.read();
        const files = [];
        let text = "";
        for (const it of items) {
          const type = it.types.find((t) => t.startsWith("image/"));
          if (type) files.push(pastedName(new File([await it.getType(type)], "image." + type.split("/")[1], { type })));
          else if (it.types.includes("text/plain")) text += await (await it.getType("text/plain")).text();
        }
        if (files.length) {
          const ok = keepFiles(inp, files);
          return ok.length ? onPaths(await upload(ok)) : undefined;
        }
        const paths = textPaths(text);
        if (paths.length) {
          const ok = keepPaths(inp, paths);
          return ok.length ? onPaths(ok) : undefined;
        }
        toast("The clipboard has no file, image or file path to use here.", true);
      } catch (err) {
        toast(err.name === "NotAllowedError" ? `The browser blocked the clipboard. Click the box and press ${PASTE_KEY}.` : err.message, true);
      }
    },
  }, "Paste");
}

const isLocalPath = (p) => p.startsWith("/") || p.startsWith("~");

// preview shows a local image, video or audio file, so the user can see
// they picked the right one. Images open in the popup viewer.
function mediaPreview(onRemove) {
  const el = h("div", { class: "in-preview", hidden: true });
  let shown = "";
  function show(p) {
    p = (p || "").trim();
    const kind = p && isLocalPath(p) ? localKind(p) : "";
    if (p === shown) return;
    shown = p;
    if (!kind) { el.hidden = true; el.replaceChildren(); return; }
    el.hidden = false;
    const url = mediaURL(p);
    const info = h("span", { class: "muted" });
    const remove = h("button", { class: "btn small quiet", type: "button", "aria-label": "Remove " + p, onclick: onRemove }, "Remove");
    const side = h("div", { class: "in-preview-side" }, info, remove);
    const bad = () => { if (shown === p) clear(el, h("p", { class: "in-preview-bad" }, "Cannot open this file. Check the path."), remove); };
    const open = () => openLocalViewer([p], 0);
    let media;
    if (kind === "image") {
      media = h("button", { class: "in-preview-media", type: "button", title: "Open larger", "aria-label": "Open the image larger", onclick: open },
        h("img", { src: url, alt: "", onerror: bad, onload: (e) => { info.textContent = `${e.target.naturalWidth} × ${e.target.naturalHeight}`; } }));
    } else if (kind === "video") {
      // A still frame; the popup plays it with controls.
      media = h("button", { class: "in-preview-media", type: "button", title: "Play", "aria-label": "Play the video", onclick: open },
        h("video", {
          src: url + "#t=0.1", preload: "metadata", muted: true, playsinline: true, tabindex: "-1", onerror: bad,
          onloadedmetadata: (e) => { info.textContent = `${e.target.videoWidth} × ${e.target.videoHeight} · ${duration(e.target.duration)}`; },
        }),
        h("span", { class: "play-badge", "aria-hidden": "true" }, "▶"));
    } else {
      media = h("audio", { src: url, controls: true, preload: "metadata", onerror: bad, onloadedmetadata: (e) => { info.textContent = duration(e.target.duration); } });
    }
    clear(el, media, side);
  }
  return { el, show };
}

function duration(sec) {
  if (!isFinite(sec)) return "";
  const m = Math.floor(sec / 60), s = Math.round(sec % 60);
  return `${m}:${String(s).padStart(2, "0")}`;
}

function pathWidget(inp, id, described, changed) {
  const isFile = inp.type === "file";
  const preview = isFile ? mediaPreview(() => setPath("")) : null;
  const update = debounce(() => preview && preview.show(box.value), 300);
  const box = h("input", { class: "input mono", id, placeholder: inp.placeholder || (isFile ? "/path/to/file" : "/path/to/folder"), "aria-describedby": described, oninput: () => { changed(); update(); }, spellcheck: "false" });
  const setPath = (p) => { box.value = p; changed(); preview && preview.show(p); };
  const browse = h("button", { class: "btn small", type: "button" }, "Browse…");
  browse.addEventListener("click", async () => {
    const p = await pick({ mode: isFile ? "file" : "dir", start: box.value || undefined, accept: inp.accept });
    if (p) setPath(p);
  });
  const el = h("div", { class: "drop" + (isFile ? " paste" : ""), tabindex: isFile ? "-1" : null },
    h("div", { class: "row" }, box, browse, isFile ? pasteButton(inp, (paths) => setPath(paths[0])) : null),
    isFile ? h("div", { class: "hint" }, `Or drop a file here, or paste one (${PASTE_KEY}).`) : null,
    preview ? preview.el : null);
  if (isFile) {
    dropTarget(el, inp, (paths) => setPath(paths[0]));
    pasteTarget(el, inp, (paths) => setPath(paths[0]));
  }
  return {
    el,
    get: () => (box.value.trim() === "" ? undefined : box.value.trim()),
    set: (v) => { box.value = v ?? ""; preview && preview.show(box.value); },
  };
}

const baseName = (p) => p.replace(/[/\\]+$/, "").split(/[/\\]/).pop() || p;
const dirName = (p) => p.slice(0, Math.max(0, p.length - baseName(p).length)).replace(/[/\\]+$/, "") || "/";
// The path cell is right-to-left so long paths cut at the start; marks keep
// the slashes in place.
const ltr = (s) => "\u200E" + s + "\u200E";

function filesWidget(inp, id, changed) {
  let paths = [];
  const rows = h("ul", { class: "file-rows", id });
  const render = () => {
    const media = paths.filter((p) => isLocalPath(p) && localKind(p));
    rows.replaceChildren(...paths.map((p, i) => {
      const kind = isLocalPath(p) ? localKind(p) : "";
      const open = kind ? () => openLocalViewer(media, media.indexOf(p)) : null;
      let thumb = null;
      if (kind === "image") thumb = h("img", { src: mediaURL(p), alt: "", loading: "lazy", onerror: (e) => e.target.replaceWith(h("span", { class: "file-thumb-bad", title: "Cannot open this file" }, "!")) });
      else if (kind === "video") thumb = h("video", { src: mediaURL(p) + "#t=0.1", preload: "metadata", muted: true, playsinline: true, tabindex: "-1" });
      else if (kind === "audio") thumb = h("span", { class: "file-thumb-kind" }, "♪");
      return h("li", null,
        h("span", { class: "file-thumb", "aria-hidden": "true" }, thumb),
        open
          ? h("button", { class: "file-open", type: "button", title: p, onclick: open, "aria-label": "Preview " + p },
            h("span", { class: "file-name" }, baseName(p)), h("span", { class: "path mono muted" }, ltr(dirName(p))))
          : h("span", { class: "file-open", title: p }, h("span", { class: "file-name" }, baseName(p)), h("span", { class: "path mono muted" }, ltr(dirName(p)))),
        h("button", { class: "btn small quiet", type: "button", "aria-label": "Remove " + p, onclick: () => { paths.splice(i, 1); render(); changed(); } }, "Remove"));
    }));
  };
  const add = (list) => { for (const p of list) if (!paths.includes(p)) paths.push(p); render(); changed(); };
  const browse = h("button", { class: "btn small", type: "button" }, "Add files…");
  browse.addEventListener("click", async () => {
    const chosen = await pick({ mode: "files", accept: inp.accept });
    if (chosen) add(chosen);
  });
  const typed = h("input", { class: "input mono", placeholder: "Or type a path and press Enter", "aria-label": "Add a path", spellcheck: "false" });
  typed.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && typed.value.trim()) {
      e.preventDefault();
      const ok = keepPaths(inp, [typed.value.trim()]);
      if (ok.length) { add(ok); typed.value = ""; }
    }
  });
  const el = h("div", { class: "drop paste", tabindex: "-1" }, rows, h("div", { class: "row" }, browse, pasteButton(inp, add), typed),
    h("div", { class: "hint" }, `Or drop files here, or paste copied images (${PASTE_KEY}). Click a file to preview it.`));
  dropTarget(el, inp, add);
  pasteTarget(el, inp, add);
  return {
    el, group: true,
    get: () => (paths.length ? [...paths] : undefined),
    set: (v) => { paths = Array.isArray(v) ? [...v] : []; render(); },
  };
}
