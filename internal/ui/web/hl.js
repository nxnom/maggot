// Small syntax highlighter for the Code tab. It splits text into tokens with
// per-language regexes and returns DOM nodes built from text, so file
// contents can never become markup. Unknown languages stay plain.
import { h } from "./dom.js";

const kw = (words) => new RegExp(`\\b(?:${words.trim().split(/\s+/).join("|")})\\b`);

// Rules are tried in order at each position; the first match wins.
const LANGS = {
  json: [
    ["key", /"(?:[^"\\\n]|\\.)*"(?=\s*:)/],
    ["str", /"(?:[^"\\\n]|\\.)*"/],
    ["lit", kw("true false null")],
    ["num", /-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b/],
  ],
  python: [
    ["com", /#.*/],
    ["str", /[rRbBfFuU]{0,2}(?:"""[\s\S]*?"""|'''[\s\S]*?''')/],
    ["str", /[rRbBfFuU]{0,2}(?:"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*')/],
    ["meta", /@[\w.]+/],
    ["kw", kw(`and as assert async await break class continue def del elif else except finally for from global
      if import in is lambda nonlocal not or pass raise return try while with yield match case`)],
    ["lit", kw("True False None self cls")],
    ["fn", /\b[A-Za-z_]\w*(?=\s*\()/],
    ["num", /\b\d[\d_]*(?:\.\d+)?(?:[eE][+-]?\d+)?\b/],
  ],
  shell: [
    ["meta", /^#!.*/m],
    ["com", /(?<![\w$#{])#.*/],
    ["str", /'[^']*'/],
    ["str", /"(?:[^"\\]|\\.)*"/],
    ["var", /\$(?:\{[^}\n]*\}|\w+|[@*#?$!0-9-])/],
    ["kw", kw(`if then else elif fi for while until do done case esac in function return exit local export
      readonly set unset shift break continue source`)],
    ["fn", /\b[\w-]+(?=\s*\(\)\s*\{)/],
    ["num", /\b\d+\b/],
  ],
  js: [
    ["meta", /^#!.*/m],
    ["com", /\/\/.*|\/\*[\s\S]*?\*\//],
    ["str", /`(?:[^`\\]|\\.)*`|"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*'/],
    ["kw", kw(`const let var function return if else for while do switch case break continue new class extends
      import export from default async await try catch finally throw typeof instanceof in of this super yield
      delete void static get set interface type enum implements as`)],
    ["lit", kw("true false null undefined NaN")],
    ["fn", /\b[A-Za-z_$][\w$]*(?=\s*\()/],
    ["num", /\b0x[\da-fA-F]+\b|\b\d[\d_]*(?:\.\d+)?\b/],
  ],
  go: [
    ["com", /\/\/.*|\/\*[\s\S]*?\*\//],
    ["str", /`[^`]*`|"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*'/],
    ["kw", kw(`break case chan const continue default defer else fallthrough for func go goto if import
      interface map package range return select struct switch type var`)],
    ["lit", kw("true false nil iota")],
    ["type", kw("string int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 bool error byte rune float32 float64 any")],
    ["fn", /\b[A-Za-z_]\w*(?=\s*\()/],
    ["num", /\b0x[\da-fA-F]+\b|\b\d[\d_]*(?:\.\d+)?\b/],
  ],
  yaml: [
    ["com", /#.*/],
    ["key", /[\w.-]+(?=:(?:\s|$))/],
    ["str", /"(?:[^"\\\n]|\\.)*"|'[^'\n]*'/],
    ["lit", kw("true false null yes no on off")],
    ["num", /\b\d+(?:\.\d+)?\b/],
  ],
  toml: [
    ["com", /#.*/],
    ["meta", /^\s*\[[^\]\n]*\]/m],
    ["key", /^\s*[\w.-]+(?=\s*=)/m],
    ["str", /"""[\s\S]*?"""|"(?:[^"\\\n]|\\.)*"|'[^'\n]*'/],
    ["lit", kw("true false")],
    ["num", /\b\d[\d_]*(?:\.\d+)?\b/],
  ],
  markdown: [
    ["com", /^```.*$/m],
    ["meta", /^#{1,6} .*/m],
    ["str", /`[^`\n]+`/],
    ["kw", /\*\*[^*\n]+\*\*/],
    ["fn", /\[[^\]\n]+\]\([^)\n]+\)/],
  ],
};

const EXT = {
  json: "json", py: "python", sh: "shell", bash: "shell", zsh: "shell",
  js: "js", mjs: "js", cjs: "js", ts: "js", tsx: "js", jsx: "js",
  go: "go", yml: "yaml", yaml: "yaml", toml: "toml", md: "markdown",
};

// language picks a language from the file name, or from the shebang line.
export function language(path, text) {
  const ext = (path.split("/").pop().split(".").slice(1).pop() || "").toLowerCase();
  if (EXT[ext]) return EXT[ext];
  const first = text.slice(0, 200).split("\n")[0];
  if (first.startsWith("#!")) {
    if (/python|uv /.test(first)) return "python";
    if (/\b(node|deno|bun)\b/.test(first)) return "js";
    if (/\b(ba|z|da)?sh\b/.test(first)) return "shell";
  }
  return null;
}

const MAX = 300 * 1024; // larger files stay plain to keep the page fast

// tokens splits text into [class, text] pairs ("" for plain text).
function tokens(text, rules) {
  const sticky = rules.map(([cls, re]) => [cls, new RegExp(re.source, re.flags.replace("g", "") + "y")]);
  const plain = /[A-Za-z_$][\w$]*|\s+|./y;
  const out = [];
  let i = 0;
  let buf = "";
  while (i < text.length) {
    let hit = null;
    for (const [cls, re] of sticky) {
      re.lastIndex = i;
      const m = re.exec(text);
      if (m && m[0].length) { hit = [cls, m[0]]; break; }
    }
    if (hit) {
      if (buf) { out.push(["", buf]); buf = ""; }
      out.push(hit);
      i += hit[1].length;
    } else {
      plain.lastIndex = i;
      const m = plain.exec(text);
      const s = m ? m[0] : text[i];
      buf += s;
      i += s.length;
    }
  }
  if (buf) out.push(["", buf]);
  return out;
}

// highlightLines returns one array of nodes per line.
export function highlightLines(text, lang) {
  const rules = LANGS[lang];
  if (!rules || text.length > MAX) return text.split("\n").map((l) => [l]);
  const lines = [[]];
  for (const [cls, s] of tokens(text, rules)) {
    const parts = s.split("\n");
    parts.forEach((part, k) => {
      if (k > 0) lines.push([]);
      if (part) lines[lines.length - 1].push(cls ? h("span", { class: "tok-" + cls }, part) : part);
    });
  }
  return lines;
}
