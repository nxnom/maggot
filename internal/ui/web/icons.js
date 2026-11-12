// File and folder icons for the file picker and the Code tab. Small inline
// SVGs built with DOM calls (no markup strings), colored per kind in CSS.

const NS = "http://www.w3.org/2000/svg";

const PAGE = "M14 3H6.5A1.5 1.5 0 0 0 5 4.5v15A1.5 1.5 0 0 0 6.5 21h11a1.5 1.5 0 0 0 1.5-1.5V8z";
const FOLD = "M14 3v5h5";

// Each kind: the shapes drawn on top of the page outline.
const GLYPHS = {
  image: [["circle", { cx: 10, cy: 11.5, r: 1.4 }], ["path", { d: "M7.5 18l3.5-3.5 2 2 1.5-1.5 2 2" }]],
  video: [["path", { d: "M10 11.5v5.5l4.5-2.75z", fill: "currentColor" }]],
  audio: [["path", { d: "M11 17V11.5l4-1V16" }], ["circle", { cx: 9.8, cy: 17, r: 1.3 }], ["circle", { cx: 13.8, cy: 16, r: 1.3 }]],
  code: [["path", { d: "M10 12l-2.2 2.5L10 17M14 12l2.2 2.5L14 17" }]],
  data: [["path", { d: "M10.5 11c-1.2 0-1.2.6-1.2 1.6s-.4 1.4-1.1 1.4c.7 0 1.1.4 1.1 1.4s0 1.6 1.2 1.6M13.5 11c1.2 0 1.2.6 1.2 1.6s.4 1.4 1.1 1.4c-.7 0-1.1.4-1.1 1.4s0 1.6-1.2 1.6" }]],
  text: [["path", { d: "M8.5 12h7M8.5 15h7M8.5 18h4.5" }]],
  pdf: [["path", { d: "M8.5 13h7M8.5 16h7" }], ["path", { d: "M8.5 18.5h3" }]],
  archive: [["path", { d: "M11 4.5h1.5M12.5 6.5H14M11 8.5h1.5M12.5 10.5H14" }], ["rect", { x: 10.8, y: 13, width: 3.4, height: 4, rx: 0.8 }]],
  file: [],
};

const EXT = {
  image: "png jpg jpeg gif webp avif heic bmp tiff tif svg ico psd",
  video: "mp4 mov mkv webm avi m4v wmv flv",
  audio: "mp3 wav m4a flac ogg opus aac aiff wma",
  pdf: "pdf",
  archive: "zip tar gz tgz bz2 xz 7z rar maggot dmg iso",
  code: "py sh bash zsh fish js mjs cjs ts tsx jsx go rs rb php java kt swift c h cc cpp hpp cs lua pl r sql html css scss vue svelte",
  data: "json jsonl ndjson yaml yml toml csv tsv xml ini env lock",
  text: "txt md markdown rst log srt vtt rtf doc docx",
};
const KIND = {};
for (const [kind, list] of Object.entries(EXT)) for (const e of list.split(" ")) KIND[e] = kind;

export function fileKind(name) {
  const base = name.toLowerCase();
  if (["dockerfile", "makefile", "justfile"].includes(base)) return "code";
  if (base === "license" || base === "readme") return "text";
  const dot = base.lastIndexOf(".");
  return dot > 0 ? KIND[base.slice(dot + 1)] || "file" : "file";
}

function el(tag, attrs) {
  const n = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v));
  return n;
}

function svg(cls, shapes) {
  const s = el("svg", { viewBox: "0 0 24 24", width: 16, height: 16, fill: "none", stroke: "currentColor",
    "stroke-width": 1.6, "stroke-linecap": "round", "stroke-linejoin": "round", class: "ic " + cls, "aria-hidden": "true", focusable: "false" });
  for (const [tag, attrs] of shapes) s.append(el(tag, attrs));
  return s;
}

// fileIcon returns an icon for a file name, picked by its extension.
export function fileIcon(name) {
  const kind = fileKind(name);
  return svg("ic-" + kind, [["path", { d: PAGE }], ["path", { d: FOLD }], ...GLYPHS[kind]]);
}

// folderIcon returns a folder icon; open shows an open folder, toolbox marks
// a maggot toolbox folder.
export function folderIcon({ open = false, toolbox = false } = {}) {
  const shapes = open
    ? [["path", { d: "M3.5 18.5V6A1.5 1.5 0 0 1 5 4.5h4l2 2h7A1.5 1.5 0 0 1 19.5 8v2", class: "ic-fill" }],
      ["path", { d: "M3.5 18.5l2.6-7.1A1.5 1.5 0 0 1 7.5 10.5h13a1 1 0 0 1 .95 1.3l-2.1 6.2a1.5 1.5 0 0 1-1.4 1H5a1.5 1.5 0 0 1-1.5-.5z", class: "ic-fill" }]]
    : [["path", { d: "M3.5 6A1.5 1.5 0 0 1 5 4.5h4l2 2h8A1.5 1.5 0 0 1 20.5 8v10A1.5 1.5 0 0 1 19 19.5H5A1.5 1.5 0 0 1 3.5 18z", class: "ic-fill" }]];
  if (toolbox) shapes.push(["path", { d: "M9 12.5h6v4.5H9zM10.5 12.5v-1.2h3v1.2", class: "ic-mark" }]);
  return svg(toolbox ? "ic-toolbox" : "ic-folder", shapes);
}
