// Output files: thumbnails for the result pane, and a popup viewer with
// previous/next navigation for every kind of file.
import { h, clear, bytes, toast } from "./dom.js";
import { post, fileURL } from "./api.js";
import { language, highlightLines } from "./hl.js";
import { renderMarkdown } from "./md.js";

const TEXT_MAX = 512 * 1024;

export function kindOf(f) {
  const [type, sub] = f.mime.split("/");
  const ext = (f.path.split(".").pop() || "").toLowerCase();
  if (type === "image" && sub !== "heic") return "image";
  if (type === "video") return "video";
  if (type === "audio") return "audio";
  if (f.mime === "application/pdf") return "pdf";
  if (type === "text" || /json|xml|yaml|toml|javascript|csv/.test(sub) ||
    ["md", "txt", "log", "csv", "tsv", "json", "yaml", "yml", "toml", "py", "sh", "js", "ts", "go", "srt", "vtt"].includes(ext)) return "text";
  return "other";
}

function extBadge(f) {
  return h("span", { class: "ext", "aria-hidden": "true" }, (f.path.split(".").pop() || "file").slice(0, 5).toUpperCase());
}

function actions(runId, f) {
  return [
    h("a", { class: "btn small", href: fileURL(runId, f.path), target: "_blank", rel: "noopener" }, "Open in new tab"),
    h("a", { class: "btn small", href: fileURL(runId, f.path, true) }, "Download"),
    h("button", { class: "btn small", type: "button", onclick: () => post("/api/reveal", { run: runId, path: f.path }).catch((e) => toast(e.message, true)) }, "Show in folder"),
  ];
}

// renderOutputs fills el with the files of a run: one big inline preview
// for a single image or video, otherwise thumbnails that open the viewer.
const ORDER = { video: 0, image: 1, audio: 2, pdf: 3, text: 4, other: 5 };

export function renderOutputs(el, runId, files) {
  // Media first, then documents, then the rest; by name within each kind.
  const sorted = [...files].sort((a, b) => (ORDER[kindOf(a)] - ORDER[kindOf(b)]) ||
    a.path.localeCompare(b.path, undefined, { sensitivity: "base", numeric: true }));
  const shown = sorted.slice(0, 200);
  const open = (i) => openViewer(runId, shown, i);
  if (shown.length === 1 && ["image", "video"].includes(kindOf(shown[0]))) {
    const f = shown[0];
    const url = fileURL(runId, f.path);
    const media = kindOf(f) === "image"
      ? h("img", { src: url, alt: f.path, onclick: () => open(0), title: "Open in viewer" })
      : h("video", { src: url, controls: true, preload: "metadata", playsinline: true });
    clear(el, h("figure", { class: "out-hero" },
      h("div", { class: "out-hero-media" }, media),
      h("figcaption", null,
        h("button", { class: "out-name", type: "button", onclick: () => open(0) }, f.path),
        h("span", { class: "muted" }, bytes(f.size)),
        h("span", { class: "out-actions" }, actions(runId, f)))));
    return;
  }
  const audio = [];
  const cards = [];
  shown.forEach((f, i) => {
    const kind = kindOf(f);
    const url = fileURL(runId, f.path);
    if (kind === "audio") {
      audio.push(h("div", { class: "out-audio-row" },
        h("button", { class: "out-name", type: "button", onclick: () => open(i), title: "Open in viewer" }, f.path),
        h("span", { class: "muted" }, bytes(f.size)),
        h("audio", { src: url, controls: true, preload: "metadata" })));
      return;
    }
    let thumb;
    if (kind === "image") thumb = h("img", { src: url, alt: "", loading: "lazy" });
    else if (kind === "video") thumb = [h("video", { src: url + "#t=0.1", preload: "metadata", muted: true, playsinline: true, tabindex: "-1" }), h("span", { class: "play-badge", "aria-hidden": "true" }, "▶")];
    else thumb = extBadge(f);
    cards.push(h("button", { class: "out-card", type: "button", onclick: () => open(i), "aria-label": `Open ${f.path}` },
      h("span", { class: "out-thumb out-thumb-" + kind }, thumb),
      h("span", { class: "out-card-meta" }, h("span", { class: "out-card-name" }, f.path), h("span", { class: "muted" }, bytes(f.size)))));
  });
  clear(el,
    audio.length ? h("div", { class: "out-audio-list" }, audio) : null,
    cards.length ? h("div", { class: "out-grid" }, cards) : null,
    files.length > shown.length ? h("p", { class: "muted" }, `Showing the first ${shown.length} of ${files.length} files. Use Show folder for the rest.`) : null);
}

// Local files (inputs chosen in the form) are shown through /api/fs/media,
// which serves only images, video and audio.
const EXT_MIME = {
  png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp",
  bmp: "image/bmp", avif: "image/avif", svg: "image/svg+xml", tif: "image/tiff", tiff: "image/tiff",
  mp4: "video/mp4", m4v: "video/mp4", mov: "video/quicktime", webm: "video/webm", mkv: "video/x-matroska",
  mp3: "audio/mpeg", m4a: "audio/mp4", wav: "audio/wav", ogg: "audio/ogg", oga: "audio/ogg", opus: "audio/ogg",
  flac: "audio/flac", aac: "audio/aac",
};

export function localFile(path) {
  const ext = (path.split(".").pop() || "").toLowerCase();
  return { path, name: path.split(/[/\\]/).pop(), mime: EXT_MIME[ext] || "application/octet-stream" };
}

export const mediaURL = (path) => "/api/fs/media?path=" + encodeURIComponent(path);

// localKind is image, video, audio or "" for a local path.
export function localKind(path) {
  const k = kindOf(localFile(path));
  return ["image", "video", "audio"].includes(k) ? k : "";
}

// openLocalViewer shows local files in the popup viewer.
export function openLocalViewer(paths, index) {
  const files = paths.map(localFile);
  openViewer(null, files, index, {
    url: (f) => mediaURL(f.path),
    actions: (f) => [h("a", { class: "btn small", href: mediaURL(f.path), target: "_blank", rel: "noopener" }, "Open in new tab")],
  });
}

// openViewer shows files[index] in a popup. ←/→ move between files, Esc closes.
// src changes where files come from (default: the run's output folder).
export function openViewer(runId, files, index, src) {
  const urlOf = src ? src.url : (f) => fileURL(runId, f.path);
  const actsOf = src ? src.actions : (f) => actions(runId, f);
  const sizeOf = (f) => (f.size === undefined ? "" : bytes(f.size));
  let i = index;
  const stage = h("div", { class: "viewer-stage" });
  const title = h("span", { class: "viewer-title" });
  const count = h("span", { class: "muted viewer-count" });
  const acts = h("span", { class: "viewer-actions" });
  const prev = h("button", { class: "viewer-nav prev", type: "button", "aria-label": "Previous file" }, "‹");
  const next = h("button", { class: "viewer-nav next", type: "button", "aria-label": "Next file" }, "›");
  const close = h("button", { class: "btn small", type: "button", "aria-label": "Close viewer" }, "Close");
  const dlg = h("dialog", { class: "viewer", "aria-label": "File viewer" },
    h("div", { class: "viewer-bar" }, title, count, acts, close),
    h("div", { class: "viewer-body" }, prev, stage, next));

  async function show() {
    const f = files[i];
    const url = urlOf(f);
    title.textContent = f.name || f.path;
    title.title = f.path;
    count.textContent = [files.length > 1 ? `${i + 1} / ${files.length}` : "", sizeOf(f)].filter(Boolean).join(" · ");
    clear(acts, actsOf(f));
    prev.hidden = next.hidden = files.length < 2;
    stage.querySelectorAll("video, audio").forEach((m) => m.pause());
    const kind = kindOf(f);
    if (src && kind !== "image" && kind !== "video" && kind !== "audio") {
      return clear(stage, h("div", { class: "viewer-none" }, extBadge(f), h("p", null, "No preview for this kind of file.")));
    }
    const missing = () => clear(stage, h("div", { class: "viewer-none" }, extBadge(f),
      h("p", null, "Cannot open this file. Check that the path is right.")));
    if (kind === "image") return clear(stage, h("img", { src: url, alt: f.path, onerror: missing }));
    if (kind === "video") return clear(stage, h("video", { src: url, controls: true, autoplay: true, playsinline: true, onerror: missing }));
    if (kind === "audio") return clear(stage, h("div", { class: "viewer-audio" }, extBadge(f), h("audio", { src: url, controls: true, autoplay: true })));
    if (kind === "pdf") return clear(stage, h("iframe", { src: url, title: f.path }));
    if (kind === "text" && f.size <= TEXT_MAX) {
      clear(stage, h("p", { class: "muted" }, "Loading…"));
      try {
        const text = (await (await fetch(url)).text()).replace(/\n$/, "");
        if (files[i] !== f) return; // moved on meanwhile
        const lang = language(f.path, text);
        if (lang === "markdown") return clear(stage, h("div", { class: "viewer-doc md-preview" }, renderMarkdown(text)));
        return clear(stage, h("pre", { class: "code viewer-doc", tabindex: "0" },
          highlightLines(text, lang).map((l) => h("span", { class: "ln" }, l, "\n"))));
      } catch (e) { return clear(stage, h("p", { class: "notice" }, e.message)); }
    }
    clear(stage, h("div", { class: "viewer-none" }, extBadge(f),
      h("p", null, kind === "text" ? "This file is too large to preview here." : "No preview for this kind of file."),
      runId ? h("a", { class: "btn green", href: fileURL(runId, f.path, true) }, "Download") : null));
  }
  const go = (d) => { i = (i + d + files.length) % files.length; show(); };
  prev.addEventListener("click", () => go(-1));
  next.addEventListener("click", () => go(1));
  close.addEventListener("click", () => dlg.close());
  dlg.addEventListener("keydown", (e) => {
    if (e.target.closest && e.target.closest("video, audio, input")) return; // let media use the arrow keys
    if (e.key === "ArrowLeft" && files.length > 1) { e.preventDefault(); go(-1); }
    if (e.key === "ArrowRight" && files.length > 1) { e.preventDefault(); go(1); }
  });
  dlg.addEventListener("click", (e) => { if (e.target === dlg) dlg.close(); }); // click on the backdrop
  dlg.addEventListener("close", () => { stage.querySelectorAll("video, audio").forEach((m) => m.pause()); dlg.remove(); });
  document.body.append(dlg);
  dlg.showModal();
  close.focus();
  show();
}
