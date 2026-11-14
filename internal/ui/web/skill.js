// The skill page. A skill is a folder of instructions for coding agents
// (SKILL.md, Agent Skills format) with optional scripts and assets. People
// read it here; agents follow it. Nothing runs from this page.
import { h, clear, toast, confirmDialog } from "./dom.js";
import { get, post, del, q } from "./api.js";
import { renderMarkdown } from "./md.js";

const AGENT_NAMES = { claude: "Claude Code", codex: "Codex", agents: "Other agents (~/.agents)" };

// skillHash is the page URL of a skill.
export const skillHash = (ref) => "#/skill/" + encodeURIComponent(ref);

// promptFor is the text the user pastes into an agent to use a skill.
export function promptFor(skill) {
  return `Use the maggot skill "${skill.name}" (read it with \`maggot skill show ${skill.ref}\`) to: `;
}

// needsLabel is a short "Needs ..." text for cards and chips.
export function needsLabel(missing) {
  return "Needs " + missing.map((x) => x.replace(/^(binary|env|tool) /, "").replace(/ \(needs .*\)$/, " setup")).join(", ");
}

// skillPage renders #/skill/<ref>. ctx gives the page what it shares with
// the tool page: loadState, loadFiles, loadChanges, stageImport, setPage.
export async function skillPage(spec, ctx, main) {
  const [ref, query] = spec.split("?");
  const params = new URLSearchParams(query || "");
  let sk;
  try {
    sk = await get(`/api/skill?ref=${q(ref)}`);
  } catch (err) {
    clear(main, h("h1", { class: "page-title" }, "Skill not found"), h("p", null, err.message), h("a", { href: "#/" }, "Back to home"));
    return;
  }
  const draft = sk.draft ? { ...sk.draft, ref: sk.ref } : null;

  // ---------- header ----------
  const copyPrompt = h("button", { class: "btn green", type: "button", title: "Copy a prompt that tells your agent to use this skill" }, "Copy prompt");
  copyPrompt.addEventListener("click", async () => {
    try { await navigator.clipboard.writeText(promptFor(sk)); toast("Prompt copied. Paste it into your agent and add the task."); }
    catch { toast("Could not copy. Select the text in the Setup tab instead.", true); }
  });
  const deleteBtn = draft ? null : h("button", { class: "btn", type: "button", onclick: async () => {
    if (!(await confirmDialog(`Delete the skill ${sk.name}?`, h("div", null,
      h("p", null, "This deletes the skill folder:"), h("p", null, h("code", null, sk.dir)),
      h("p", null, "Links to it in your agents' skills folders are removed too.")), "Delete skill", true))) return;
    try { await del(`/api/tool?kind=skill&ref=${q(sk.ref)}`); toast(`Deleted ${sk.name}.`); await ctx.loadState(); location.hash = "#/"; } catch (e) { toast(e.message, true); }
  } }, "Delete");
  const chips = h("div", { class: "reqs", "aria-label": "Ready to use" }, readyChips(sk, () => select("setup")));
  const desc = h("p", { class: "tp-desc", title: sk.description }, sk.description);
  const more = h("button", { class: "tp-more", type: "button", hidden: true }, "More");
  more.addEventListener("click", () => { more.textContent = desc.classList.toggle("open") ? "Less" : "More"; });
  const header = h("header", { class: "tp-head" + (draft ? " draft" : "") },
    h("div", { class: "tp-titlebar" },
      h("div", { class: "tp-title" },
        h("nav", { class: "tp-crumbs", "aria-label": "Breadcrumb" },
          h("a", { href: "#/" }, "Skills"), h("span", { "aria-hidden": "true" }, "/"), h("span", null, draft ? "Drafts" : sk.toolbox), h("span", { "aria-hidden": "true" }, "/")),
        h("h1", null, sk.name),
        sk.version ? h("span", { class: "tp-version" }, "v" + sk.version) : null,
        h("span", { class: "kind-chip" }, "Skill"),
        draft ? h("span", { class: "tp-draft-badge" }, "Draft") : null,
        chips),
      h("div", { class: "tp-actions" },
        draft ? null : copyPrompt,
        h("button", { class: "btn", type: "button", onclick: () => post("/api/tool/open", { ref: sk.ref, kind: "skill" }).catch((e) => toast(e.message, true)) }, "Show folder"),
        h("a", { class: "btn", href: `/api/export?kind=skill&ref=${q(sk.ref)}` }, "Export"),
        draft ? null : h("button", { class: "btn", type: "button", title: "Hide it from search, lists and agents. The folder is kept.", onclick: () => ctx.archive(sk.ref, sk.name) }, "Archive"),
        deleteBtn)),
    h("div", { class: "tp-desc-row" }, desc, more),
    draft ? draftNote(sk, draft, ctx, () => select("changes")) : null,
    sk.has_draft ? h("div", { class: "draft-note linked" },
      h("p", null, `You are working on a new version of this skill: draft${sk.has_draft.version ? " v" + sk.has_draft.version : ""}. Importing the draft replaces this skill.`),
      h("div", { class: "row" },
        h("a", { class: "btn", href: skillHash(sk.has_draft.ref) }, "Open draft"),
        h("a", { class: "btn quiet", href: skillHash(sk.has_draft.ref) + "?tab=changes" }, "See changes"))) : null);
  requestAnimationFrame(() => { more.hidden = desc.scrollWidth <= desc.clientWidth + 1; });

  // ---------- panels ----------
  const body = sk.body.replace(/<!--[\s\S]*?-->/g, ""); // notes for agents, not for people
  const steps = h("div", { class: "tp-docs skill-steps" },
    h("p", { class: "muted skill-intro" },
      "Your coding agent follows these steps. Paths like ", h("code", null, "scripts/…"), " are relative to the skill folder."),
    body.trim() ? renderMarkdown(body) : h("p", { class: "muted" }, "SKILL.md has no instructions yet."));
  const files = h("div", { class: "code-browser" });
  const setup = h("div", { class: "tp-docs" });
  const changes = h("div", { class: "tp-split tp-split-list" });

  const tabDefs = [
    ["steps", "Instructions"], ["code", "Files"],
    ["setup", ["Setup", sk.check.ready ? null : h("span", { class: "tab-count bad" }, String(sk.check.missing.length))]],
    draft && draft.of ? ["changes", "Changes"] : null,
  ].filter(Boolean);
  const panels = { steps, code: files, setup, changes };
  const tabButtons = {};
  const panelEls = {};
  const tabBar = h("div", { class: "tp-tabs", role: "tablist", "aria-label": "Skill sections" },
    tabDefs.map(([id, label]) => {
      const b = h("button", { type: "button", role: "tab", id: "tab-" + id, "aria-controls": "panel-" + id }, label);
      b.addEventListener("click", () => select(id));
      tabButtons[id] = b;
      return b;
    }));
  tabBar.addEventListener("keydown", (e) => {
    if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
    e.preventDefault();
    const ids = tabDefs.map(([id]) => id);
    const next = ids[(ids.indexOf(current) + (e.key === "ArrowRight" ? 1 : ids.length - 1)) % ids.length];
    select(next);
    tabButtons[next].focus();
  });
  // The panel classes reuse the tool page layout: Files is the Code browser.
  const panelClass = { steps: "docs", code: "code", setup: "docs", changes: "changes" };
  for (const [id] of tabDefs) {
    panelEls[id] = h("section", { class: "tp-panel tp-panel-" + panelClass[id], role: "tabpanel", id: "panel-" + id, "aria-labelledby": "tab-" + id, hidden: true }, panels[id]);
  }
  let current = null;
  const loaded = {};
  const page = { ref: sk.ref, kind: "skill" };
  function select(id) {
    if (!panelEls[id]) id = "steps";
    current = id;
    for (const [tid, b] of Object.entries(tabButtons)) {
      const on = tid === id;
      b.setAttribute("aria-selected", String(on));
      b.tabIndex = on ? 0 : -1;
      panelEls[tid].hidden = !on;
    }
    if (id === "code" && !loaded.code) { loaded.code = true; ctx.loadFiles(page, files, params.get("file")); }
    if (id === "changes" && !loaded.changes) { loaded.changes = true; ctx.loadChanges(page, changes, params.get("file")); }
    if (id === "setup" && !loaded.setup) { loaded.setup = true; renderSetup(sk, setup, draft, ctx); }
    history.replaceState(null, "", skillHash(sk.ref) + (id === "steps" ? "" : "?tab=" + id));
  }

  main.classList.add("tool-main");
  clear(main, h("div", { class: "tp" }, header, tabBar, Object.values(panelEls)));
  ctx.setPage({ cleanup: () => main.classList.remove("tool-main") });
  select(params.get("tab") || "steps");
}

function readyChips(sk, openSetup) {
  if (sk.check.ready) return h("span", { class: "req", title: "Every tool, program and secret it needs is there" }, "Ready");
  const chip = h("button", { class: "req bad req-btn", type: "button", title: sk.check.missing.join("\n"), onclick: openSetup }, needsLabel(sk.check.missing));
  return chip;
}

function draftNote(sk, draft, ctx, showChanges) {
  const remove = h("button", { class: "btn small quiet danger", type: "button" }, "Delete draft");
  remove.addEventListener("click", async () => {
    if (!(await confirmDialog(`Delete the draft of ${sk.name}?`,
      h("div", null, h("p", null, "This deletes the draft folder:"), h("p", null, h("code", null, draft.path))), "Delete draft", true))) return;
    try { await del(`/api/drafts?kind=skill&ref=${q(draft.ref)}`); await ctx.loadState(); location.hash = "#/"; } catch (e) { toast(e.message, true); }
  });
  const of = draft.of;
  if (!of) {
    return h("div", { class: "draft-note" },
      h("p", null, "New skill, not in your toolbox yet. Read it here, then import it.", h("br"), h("code", null, draft.path)),
      h("div", { class: "row" },
        h("button", { class: "btn green", type: "button", onclick: () => ctx.stageImport(draft.path, draft) }, "Import…"),
        remove));
  }
  return h("div", { class: "draft-note linked" },
    h("p", null, "New version of ", h("a", { href: skillHash(of.ref) }, of.name),
      ` in ${of.toolbox}: installed ${of.version ? "v" + of.version : "(no version)"} → this draft ${sk.version ? "v" + sk.version : "(no version)"}. Importing replaces the installed skill.`,
      h("br"), h("code", null, draft.path)),
    h("div", { class: "row" },
      h("button", { class: "btn green", type: "button", onclick: () => ctx.stageImport(draft.path, draft) }, `Update ${of.name}…`),
      h("button", { class: "btn", type: "button", onclick: showChanges }, "See changes"),
      remove));
}

// renderSetup shows what the skill needs, its secrets, and which agents
// have it installed.
function renderSetup(sk, el, draft, ctx) {
  const c = sk.check;
  const row = (okState, label, detail, extra) => h("li", { class: "need" + (okState ? "" : " bad") },
    h("span", { class: "need-mark", "aria-label": okState ? "ok" : "missing" }, okState ? "✓" : "✕"),
    h("span", { class: "need-label" }, label), detail ? h("span", { class: "need-detail muted" }, detail) : null, extra || null);
  const needs = [];
  for (const t of c.tools) {
    if (!t.found) needs.push(row(false, ["Tool ", h("code", null, t.name)], "Not in your toolboxes. Ask your agent to find or build it."));
    else needs.push(row(!(t.missing || []).length, ["Tool ", h("a", { href: "#/tool/" + encodeURIComponent(t.ref) }, t.ref)],
      (t.missing || []).length ? "Needs " + t.missing.join(", ") + ". Open the tool to fix it." : "Ready"));
  }
  for (const b of c.requires) needs.push(row(b.found, ["Program ", h("code", null, b.bin)], b.found ? b.path : "Not on PATH. Install it, then reload this page."));
  // A saved secret can change "ready", which the header shows too.
  const secretRows = c.env.map((e) => secretRow(sk, e, () => ctx.refresh()));

  const agentsPanel = draft ? null : h("section", { class: "panel" },
    h("div", { class: "panel-head" }, h("h2", null, "Agents")),
    h("div", { class: "panel-body" },
      h("p", { class: "muted setup-help" }, "Installing links this skill folder into the agent's skills folder, so the agent loads it on its own. ",
        "Without it, agents that have the maggot skill still find this skill with ", h("code", null, "maggot search"), "."),
      h("ul", { class: "need-list" }, sk.agents.map((a) => {
        const installed = a.state === "linked" || a.state === "copied";
        const btn = h("button", { class: "btn small" + (installed ? " quiet" : ""), type: "button" }, installed ? "Remove" : "Install");
        btn.addEventListener("click", async () => {
          btn.disabled = true;
          try {
            const res = await post("/api/skill/agents", { ref: sk.ref, agent: a.agent, install: !installed });
            sk.agents = res.agents;
            toast(installed ? `Removed from ${AGENT_NAMES[a.agent] || a.agent}.` : `Installed for ${AGENT_NAMES[a.agent] || a.agent}.`);
            renderSetup(sk, el, draft, ctx);
          } catch (e) { toast(e.message, true); btn.disabled = false; }
        });
        const detail = { linked: "Installed (linked to this folder)", copied: "Installed (a copy)", none: "Not installed",
          other: "A different skill with this name is there" }[a.state];
        return h("li", { class: "need" + (installed ? "" : " off") },
          h("span", { class: "need-mark", "aria-hidden": "true" }, installed ? "✓" : "–"),
          h("span", { class: "need-label" }, AGENT_NAMES[a.agent] || a.agent),
          h("span", { class: "need-detail muted", title: a.path }, detail),
          a.state === "other" ? null : btn);
      }))));

  clear(el,
    h("section", { class: "panel" },
      h("div", { class: "panel-head" }, h("h2", null, c.ready ? "Everything it needs is ready" : "What it needs")),
      h("div", { class: "panel-body" },
        needs.length || secretRows.length ? null : h("p", { class: "muted setup-help" }, "This skill does not list any tools, programs or secrets."),
        needs.length ? h("ul", { class: "need-list" }, needs) : null,
        secretRows.length ? h("div", { class: "setup-secrets" },
          h("h3", { class: "group-title" }, "Secrets"),
          h("p", { class: "muted setup-help" }, "Saved in your system keychain, never in the skill folder. Agents pass them to the skill's scripts with ",
            h("code", null, `maggot skill exec ${sk.name} -- …`), "."),
          secretRows) : null)),
    agentsPanel,
    h("section", { class: "panel" },
      h("div", { class: "panel-head" }, h("h2", null, "Use it")),
      h("div", { class: "panel-body" },
        h("dl", { class: "kv" },
          h("dt", null, "Folder"), h("dd", null, h("code", null, sk.dir)),
          h("dt", null, "Agents read it with"), h("dd", null, h("code", null, `maggot skill show ${sk.ref}`)),
          draft ? null : h("dt", null, "Prompt"), draft ? null : h("dd", null, h("code", { class: "prompt-text" }, promptFor(sk) + "…"))))));
}

function secretRow(sk, s, done) {
  const box = h("input", { class: "input mono", type: "password", autocomplete: "off", "aria-label": s.name, placeholder: s.set ? "Saved. Type to replace." : "Not set" });
  const save = h("button", { class: "btn small", type: "button" }, "Save");
  save.addEventListener("click", async () => {
    try { await post("/api/skill/secret", { ref: sk.ref, name: s.name, value: box.value }); toast(`Saved ${s.name}.`); done(); } catch (e) { toast(e.message, true); }
  });
  const clr = s.source === "store" ? h("button", { class: "btn small quiet danger", type: "button", onclick: async () => {
    try { await del(`/api/skill/secret?ref=${q(sk.ref)}&name=${q(s.name)}`); done(); } catch (e) { toast(e.message, true); }
  } }, "Clear") : null;
  return h("div", { class: "field" },
    h("div", { class: "label" }, h("code", null, s.name), s.set ? h("span", { class: "req-mark" }, s.source === "env" ? "set in the environment" : "saved") : h("span", { class: "req-mark bad-text" }, "not set")),
    h("div", { class: "row" }, box, save, clr));
}
