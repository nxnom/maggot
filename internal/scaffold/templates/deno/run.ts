#!/usr/bin/env -S deno run --allow-read --allow-write --allow-env
// {{NAME}}: {{DESCRIPTION}}
// Standalone CLI. Run directly (./run.ts --help) or with `maggot run {{ID}}`.
// Import packages with "npm:" or "jsr:" specifiers; Deno caches them, no node_modules.

function event(fields: Record<string, unknown>) {
  // Progress/output events for the maggot UI. No-op without MAGGOT_EVENTS.
  const path = Deno.env.get("MAGGOT_EVENTS");
  if (path) Deno.writeTextFileSync(path, JSON.stringify(fields) + "\n", { append: true });
}

function usage() {
  console.error("usage: run.ts [--out DIR] [--upper] FILE...");
}

let out = ".";
let upper = false;
const files: string[] = [];
const args = [...Deno.args];
while (args.length) {
  const a = args.shift()!;
  if (a === "--out") out = args.shift() ?? ".";
  else if (a === "--upper") upper = true;
  else if (a === "-h" || a === "--help") { usage(); Deno.exit(0); }
  else if (a.startsWith("-")) { console.error(`error: unknown flag ${a}`); usage(); Deno.exit(2); }
  else files.push(a);
}
if (files.length === 0) { usage(); Deno.exit(2); }

Deno.mkdirSync(out, { recursive: true });
files.forEach((src, i) => {
  let text: string;
  try {
    text = Deno.readTextFileSync(src);
  } catch {
    console.error(`error: ${src} is not a file`);
    Deno.exit(2);
  }
  if (upper) text = text.toUpperCase();
  const base = src.split(/[\\/]/).pop()!;
  const dot = base.lastIndexOf(".");
  const name = dot > 0 ? `${base.slice(0, dot)}.out${base.slice(dot)}` : `${base}.out`;
  const dest = `${out}/${name}`;
  Deno.writeTextFileSync(dest, text);
  event({ type: "output", path: Deno.realPathSync(dest) });
  event({ type: "progress", value: (i + 1) / files.length });
  console.log(dest);
});
