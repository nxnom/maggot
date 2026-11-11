// A small Markdown renderer for tool READMEs. It builds DOM nodes directly
// (never HTML strings), so a README cannot inject markup or scripts. Raw HTML
// in the source is shown as text. Links work only for http(s), mailto and #.
//
// Supported: headings, paragraphs, emphasis, inline code, fenced and indented
// code blocks, block quotes, nested lists (incl. task lists), tables, rules,
// links and autolinks. Images show their alt text as a link.
import { h } from "./dom.js";

export function renderMarkdown(src) {
  const lines = src.replace(/\r\n?/g, "\n").replace(/\t/g, "    ").split("\n");
  return h("div", { class: "md" }, blocks(lines));
}

const isBlank = (l) => l.trim() === "";
const fenceRe = /^ {0,3}(`{3,}|~{3,})\s*([\w+-]*)/;
const headingRe = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const hrRe = /^ {0,3}([-*_])(\s*\1){2,}\s*$/;
const listRe = /^( *)([-*+]|\d{1,9}[.)])\s+(.*)$/;
const tableSepRe = /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$/;

function blocks(lines) {
  const out = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (isBlank(line)) { i++; continue; }

    let m;
    if ((m = line.match(fenceRe))) {
      const fence = m[1];
      const body = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith(fence)) body.push(lines[i++]);
      i++; // closing fence
      out.push(codeBlock(body.join("\n"), m[2]));
      continue;
    }
    if (/^ {4}/.test(line)) {
      const body = [];
      while (i < lines.length && (/^ {4}/.test(lines[i]) || (isBlank(lines[i]) && /^ {4}/.test(lines[i + 1] || "")))) {
        body.push(lines[i++].slice(4));
      }
      out.push(codeBlock(body.join("\n"), ""));
      continue;
    }
    if ((m = line.match(headingRe))) {
      out.push(h("h" + Math.min(m[1].length + 1, 6), null, inline(m[2])));
      i++;
      continue;
    }
    if (hrRe.test(line)) { out.push(h("hr")); i++; continue; }
    if (/^ {0,3}>/.test(line)) {
      const body = [];
      while (i < lines.length && !isBlank(lines[i]) && (/^ {0,3}>/.test(lines[i]) || body.length)) {
        body.push(lines[i++].replace(/^ {0,3}> ?/, ""));
      }
      out.push(h("blockquote", null, blocks(body)));
      continue;
    }
    if (listRe.test(line)) {
      const [node, next] = list(lines, i);
      out.push(node);
      i = next;
      continue;
    }
    if (line.includes("|") && i + 1 < lines.length && tableSepRe.test(lines[i + 1]) && lines[i + 1].includes("-")) {
      const [node, next] = table(lines, i);
      out.push(node);
      i = next;
      continue;
    }
    // paragraph
    const para = [];
    while (i < lines.length && !isBlank(lines[i]) && !fenceRe.test(lines[i]) && !headingRe.test(lines[i]) &&
      !hrRe.test(lines[i]) && !/^ {0,3}>/.test(lines[i]) && !(para.length && listRe.test(lines[i]))) {
      para.push(lines[i++].trim());
    }
    out.push(h("p", null, inline(para.join("\n"))));
  }
  return out;
}

function codeBlock(text, lang) {
  return h("pre", { class: "md-code" }, h("code", { "data-lang": lang || null }, text));
}

// list parses a list starting at lines[i]; nested items are lines indented
// past the item's text column.
function list(lines, i) {
  const first = lines[i].match(listRe);
  const baseIndent = first[1].length;
  const ordered = /\d/.test(first[2]);
  const items = [];
  let cur = null;
  while (i < lines.length) {
    const line = lines[i];
    const m = line.match(listRe);
    if (m && m[1].length === baseIndent && /\d/.test(m[2]) === ordered) {
      cur = { lines: [m[3]], start: ordered ? parseInt(m[2], 10) : null };
      items.push(cur);
      i++;
      continue;
    }
    const indent = line.match(/^ */)[0].length;
    if (isBlank(line)) {
      // a blank line ends the list unless the next line continues it
      const next = lines[i + 1] || "";
      const nm = next.match(listRe);
      const sameList = nm && nm[1].length === baseIndent && /\d/.test(nm[2]) === ordered;
      if (sameList || (!nm && !isBlank(next) && next.match(/^ */)[0].length > baseIndent)) {
        cur.lines.push("");
        i++;
        continue;
      }
      break;
    }
    if (indent > baseIndent || (cur && !m && !isBlank(line) && !headingRe.test(line) && !fenceRe.test(line))) {
      cur.lines.push(line.slice(Math.min(indent, baseIndent + 2)));
      i++;
      continue;
    }
    break;
  }
  const el = h(ordered ? "ol" : "ul", { start: ordered && items[0].start !== 1 ? items[0].start : null },
    items.map((it) => {
      let body = it.lines;
      let box = null;
      const task = body[0].match(/^\[([ xX])\]\s+(.*)$/);
      if (task) {
        box = h("input", { type: "checkbox", disabled: true, checked: task[1] !== " ", "aria-label": task[1] !== " " ? "done" : "not done" });
        body = [task[2], ...body.slice(1)];
      }
      const hasBlocks = body.slice(1).some((l) => listRe.test(l) || isBlank(l) || /^ {4}/.test(l) || fenceRe.test(l));
      const content = hasBlocks ? blocks(body) : [inline(body.join("\n"))];
      return h("li", { class: box ? "task" : null }, box, content);
    }));
  return [el, i];
}

function splitRow(line) {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
  return s.split(/(?<!\\)\|/).map((c) => c.trim().replace(/\\\|/g, "|"));
}

function table(lines, i) {
  const head = splitRow(lines[i]);
  const aligns = splitRow(lines[i + 1]).map((c) => (c.startsWith(":") && c.endsWith(":") ? "center" : c.endsWith(":") ? "right" : null));
  i += 2;
  const rows = [];
  while (i < lines.length && !isBlank(lines[i]) && lines[i].includes("|")) rows.push(splitRow(lines[i++]));
  const cell = (tag, text, j) => h(tag, { style: aligns[j] ? `text-align:${aligns[j]}` : null }, inline(text || ""));
  return [h("div", { class: "md-table" }, h("table", null,
    h("thead", null, h("tr", null, head.map((c, j) => cell("th", c, j)))),
    h("tbody", null, rows.map((r) => h("tr", null, head.map((_, j) => cell("td", r[j], j))))))), i];
}

function safeHref(url) {
  const u = url.trim();
  if (u.startsWith("#")) return u;
  try {
    const p = new URL(u);
    return ["http:", "https:", "mailto:"].includes(p.protocol) ? p.href : null;
  } catch { return null; }
}

function link(text, url) {
  const href = safeHref(url);
  if (!href) return h("span", null, text);
  return h("a", { href, target: href.startsWith("#") ? null : "_blank", rel: "noopener noreferrer" }, text);
}

// Inline spans, in priority order. Each match becomes nodes; text between
// matches stays text.
const INLINE = new RegExp([
  "(`+)([\\s\\S]*?[^`]|[^`])\\1(?!`)",                 // 1,2 code
  "!\\[([^\\]]*)\\]\\(([^)\\s]+)(?:\\s+\"[^\"]*\")?\\)", // 3,4 image
  "\\[([^\\]]+)\\]\\(([^)\\s]+)(?:\\s+\"[^\"]*\")?\\)",  // 5,6 link
  "<((?:https?|mailto):[^>\\s]+)>",                    // 7 autolink
  "\\*\\*(?=\\S)([\\s\\S]*?\\S)\\*\\*",                // 8 bold
  "__(?=\\S)([\\s\\S]*?\\S)__",                        // 9 bold
  "\\*(?=[^\\s*])([\\s\\S]*?[^\\s*])\\*",              // 10 italic
  "(?<![\\w])_(?=[^\\s_])([\\s\\S]*?[^\\s_])_(?![\\w])", // 11 italic
  "~~(?=\\S)([\\s\\S]*?\\S)~~",                        // 12 strike
  "(https?:\\/\\/[^\\s<]*[^\\s<.,;:!?)\\]'\"])",          // 13 bare URL
  "\\\\([\\\\`*_{}\\[\\]()#+\\-.!|~<>])",               // 14 escape
  "( {2,}|\\\\)\\n",                                    // 15 hard break
].join("|"), "g");

function inline(text) {
  const out = [];
  let last = 0;
  // A fresh regex per call: inline() recurses, and a shared global regex
  // would have its lastIndex reset by the inner call (endless loop).
  const re = new RegExp(INLINE.source, "g");
  let m;
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index));
    last = re.lastIndex;
    if (m[1] !== undefined) out.push(h("code", null, m[2].replace(/^ (.*) $/, "$1")));
    else if (m[4] !== undefined) out.push(link(m[3] ? `[image: ${m[3]}]` : "[image]", m[4]));
    else if (m[6] !== undefined) out.push(link(inline(m[5]), m[6]));
    else if (m[7] !== undefined) out.push(link(m[7], m[7]));
    else if (m[8] !== undefined) out.push(h("strong", null, inline(m[8])));
    else if (m[9] !== undefined) out.push(h("strong", null, inline(m[9])));
    else if (m[10] !== undefined) out.push(h("em", null, inline(m[10])));
    else if (m[11] !== undefined) out.push(h("em", null, inline(m[11])));
    else if (m[12] !== undefined) out.push(h("del", null, inline(m[12])));
    else if (m[13] !== undefined) out.push(link(m[13], m[13]));
    else if (m[14] !== undefined) out.push(m[14]);
    else if (m[15] !== undefined) out.push(h("br"));
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}
