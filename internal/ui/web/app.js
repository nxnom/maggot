import { h, clear, toast, openDialog, confirmDialog, bytes, ago, duration, store, debounce } from "./dom.js";
import { get, post, del, q, api } from "./api.js";
import { pick } from "./picker.js";
import { buildForm } from "./form.js";
import { RunView } from "./run.js";
import { renderMarkdown } from "./md.js";
import { language, highlightLines } from "./hl.js";


const side = document.getElementById("side");
const main = document.getElementById("main");
let state = null;
let filter = { text: "", toolbox: store("filter:toolbox") || "", refs: null };
let page = null; // cleanup for the current page

// ---------- data ----------

async function loadState() {
  state = await get("/api/state");
  if (filter.toolbox && !state.toolboxes.some((t) => t.name === filter.toolbox)) filter.toolbox = "";
  renderSide();
}

// ---------- routing ----------

function route() {
  if (page && page.cleanup) page.cleanup();
  page = null;
  const hash = decodeURIComponent(location.hash.slice(1));
  if (hash.startsWith("/tool/")) return toolPage(hash.slice(6));
  if (hash === "/toolboxes") return toolboxesPage();
  return homePage();
}
window.addEventListener("hashchange", () => { renderSide(); route(); main.focus({ preventScroll: true }); });

// ---------- sidebar ----------

function renderSide() {
  if (!state) return;
  const current = decodeURIComponent(location.hash.slice(1)).replace(/^\/tool\//, "").split("?")[0];
  const search = h("input", {
    class: "search", type: "search", placeholder: "Search tools", "aria-label": "Search tools",
    value: filter.text, oninput: debounce(async (e) => {
      filter.text = e.target.value;
      filter.refs = filter.text.trim() ? (await get(`/api/search?q=${q(filter.text)}`)).refs : null;
      renderList();
    }, 150),
  });
  const boxes = state.toolboxes.filter((t) => !t.error);
  const tbSelect = boxes.length > 1 ? h("select", {
    class: "select", "aria-label": "Toolbox", onchange: (e) => { filter.toolbox = e.target.value; store("filter:toolbox", filter.toolbox); renderList(); },
  }, h("option", { value: "" }, "All toolboxes"), boxes.map((t) => h("option", { value: t.name, selected: filter.toolbox === t.name }, t.name))) : null;

  const list = h("ul", { class: "tool-list", "aria-label": "Tools" });
  function renderList() {
    let tools = state.tools.filter((t) => !filter.toolbox || t.toolbox === filter.toolbox);
    const items = [];
    if (filter.refs) {
      const byRef = new Map(tools.map((t) => [t.ref, t]));
      tools = filter.refs.map((r) => byRef.get(r)).filter(Boolean);
      tools.forEach((t) => items.push(toolItem(t)));
      if (!tools.length) items.push(h("li", { class: "muted", style: "padding:8px" }, "No tools match."));
    } else {
      const groups = new Map();
      for (const t of tools) {
        const g = t.categories[0] || "Other";
        if (!groups.has(g)) groups.set(g, []);
        groups.get(g).push(t);
      }
      const names = [...groups.keys()].sort((a, b) => (a === "Other") - (b === "Other") || a.localeCompare(b));
      for (const g of names) {
        if (names.length > 1) items.push(h("li", { class: "tool-group", "aria-hidden": "true" }, g));
        groups.get(g).sort((a, b) => a.name.localeCompare(b.name)).forEach((t) => items.push(toolItem(t)));
      }
    }
    clear(list, items);
  }
  function toolItem(t) {
    const showBox = boxes.length > 1 && !filter.toolbox;
    return h("li", null, h("a", {
      class: "tool-link", href: "#/tool/" + encodeURIComponent(t.ref), "aria-current": current === t.ref ? "page" : null,
    }, h("span", { class: "name" }, t.name), h("span", { class: "desc" }, showBox ? `${t.toolbox}: ${t.description}` : t.description)));
  }
  renderList();

  const drafts = state.drafts.length ? h("ul", { class: "tool-list drafts", "aria-label": "Drafts" },
    h("li", { class: "tool-group", "aria-hidden": "true" }, "Drafts"),
    state.drafts.map((d) => h("li", null, h("a", {
      class: "tool-link" + (d.error ? " broken" : ""), href: "#/tool/" + encodeURIComponent(d.ref),
      "aria-current": current === d.ref ? "page" : null, title: d.path,
    }, h("span", { class: "name" }, d.name || d.id), h("span", { class: "desc" }, d.error ? "Invalid tool.json" : d.kind === "draft" ? d.description : d.path))))) : null;

  clear(side,
    h("a", { class: "wordmark", href: "#/" }, "maggot", h("small", null, state.version)),
    drafts,
    state.toolboxes.length ? [search, tbSelect, list] : h("p", { class: "muted", style: "margin:0;flex:1" }, "No toolbox yet."),
    h("div", { class: "side-foot" },
      h("a", { class: "btn small", href: "#/toolboxes" }, "Toolboxes"),
      state.toolboxes.length ? h("button", { class: "btn small", type: "button", onclick: importDialog }, "Import tool") : null,
      h("button", { class: "btn small", type: "button", onclick: tryDialog }, "Try a tool folder…")));
}

// tryDialog opens a tool folder as a draft, without importing it.
function tryDialog() {
  const box = h("input", { class: "input mono", placeholder: "/path/to/tool-folder", "aria-label": "Tool folder path", spellcheck: "false" });
  const open = async () => {
    try {
      const res = await post("/api/drafts", { path: box.value });
      await loadState();
      location.hash = "#/tool/" + encodeURIComponent(res.ref);
    } catch (err) { toast(err.message, true); return false; }
  };
  box.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); open().then((keep) => { if (keep !== false) dlg.close(); }); } });
  const dlg = openDialog({
    title: "Try a tool folder",
    body: h("div", null,
      h("p", { style: "margin-top:0" }, "Paste the folder an agent gave you (the one with tool.json). You can run it here and import it when you are happy. It is not added to your toolboxes."),
      h("div", { class: "row" }, box, h("button", { class: "btn small", type: "button", onclick: async () => {
        const p = await pick({ mode: "dir", title: "Choose a tool folder", start: box.value || undefined });
        if (p) box.value = p;
      } }, "Browse…"))),
    actions: [{ label: "Cancel", class: "quiet" }, { label: "Open", class: "green", onClick: open }],
  });
  box.focus();
}

// ---------- home ----------

async function homePage() {
  if (!state.toolboxes.length) return emptyPage();
  const toolsCount = state.tools.length;
  const recent = h("div");
  clear(main,
    h("h1", { class: "page-title" }, toolsCount ? "Pick a tool" : "This toolbox is empty"),
    toolsCount
      ? h("p", { class: "muted", style: "margin:0 0 24px" }, `${toolsCount} tool${toolsCount === 1 ? "" : "s"} in ${state.toolboxes.length} toolbox${state.toolboxes.length === 1 ? "" : "es"}. Choose one on the left, or search by what you want to do.`)
      : h("div", { style: "max-width:62ch" },
          h("p", null, "Ask your coding agent to build a tool. With the maggot skill it creates one with ", h("code", null, "maggot new"), ", tests it, and adds it here. You can also import a .maggot file."),
          h("button", { class: "btn green", type: "button", onclick: importDialog }, "Import tool")),
    problemsNotice(),
    recent);
  const { runs } = await get("/api/runs?limit=8");
  if (!runs.length) return;
  clear(recent,
    h("h2", { style: "font-size:16px;margin:28px 0 8px" }, "Recent runs"),
    h("div", { class: "panel" }, h("ul", { class: "history" }, runs.map((r) => h("li", null,
      h("a", { href: `#/tool/${encodeURIComponent(r.ref)}?run=${r.id}`, style: "display:grid;grid-template-columns:auto 1fr auto;gap:10px;padding:9px 16px;text-decoration:none;color:inherit" },
        h("span", { class: "status " + r.status }), h("span", null, r.tool_name), h("span", { class: "muted" }, ago(r.started))))))));
}

function problemsNotice() {
  const broken = (state.problems || []).filter((p) => p.path);
  if (!broken.length) return null;
  return h("details", { class: "notice" },
    h("summary", null, `${broken.length} tool folder${broken.length === 1 ? " has" : "s have"} problems and ${broken.length === 1 ? "is" : "are"} hidden`),
    h("ul", null, broken.map((p) => h("li", null, h("code", null, p.path), h("pre", { class: "mono", style: "white-space:pre-wrap;margin:4px 0 8px" }, p.error)))));
}

function emptyPage() {
  clear(main, h("section", { class: "empty" },
    h("h1", null, "Choose a toolbox folder"),
    h("p", null, "A toolbox is a plain folder of tools. Pick one you already have, such as a git checkout, or an empty folder to start a new one."),
    h("button", { class: "btn green", type: "button", onclick: addToolbox }, "Choose folder…")));
}

async function addToolbox() {
  const path = await pick({ mode: "dir", title: "Choose a toolbox folder" });
  if (!path) return;
  try {
    await post("/api/toolboxes", { path });
  } catch (err) {
    if (err.status !== 409 || !err.data.can_create) return toast(err.message, true);
    const yes = await confirmDialog("Create a toolbox here?",
      h("div", null, h("p", null, "This folder is not a toolbox yet. maggot will add a maggot.json file and a tools folder to:"), h("p", null, h("code", null, path))),
      "Create toolbox");
    if (!yes) return;
    try { await post("/api/toolboxes", { path, create: true }); } catch (e2) { return toast(e2.message, true); }
  }
  toast("Toolbox added.");
  await loadState();
  route();
}

// ---------- toolboxes ----------

function toolboxesPage() {
  const rows = state.toolboxes.map((t) => h("tr", null,
    h("td", null, h("strong", null, t.name), t.default ? h("span", { class: "badge" }, "default") : null),
    h("td", null, h("code", null, t.path), t.error ? h("div", { class: "notice", style: "margin:6px 0 0" }, t.error) : null),
    h("td", null, t.error ? "" : String(t.tools)),
    h("td", { style: "white-space:nowrap" },
      t.default || t.error ? null : h("button", { class: "btn small", type: "button", onclick: async () => {
        try { await post(`/api/toolboxes/${q(t.name)}/default`); await loadState(); route(); } catch (e) { toast(e.message, true); }
      } }, "Make default"),
      h("button", { class: "btn small quiet danger", type: "button", onclick: async () => {
        if (!(await confirmDialog(`Remove ${t.name}?`, "maggot stops using this toolbox. The folder and its tools stay on disk.", "Remove", true))) return;
        try { await del(`/api/toolboxes/${q(t.name)}`); await loadState(); route(); } catch (e) { toast(e.message, true); }
      } }, "Remove"))));
  clear(main,
    h("h1", { class: "page-title" }, "Toolboxes"),
    h("p", { class: "muted", style: "margin:0;max-width:70ch" }, "Folders maggot reads tools from. New and imported tools go to the default toolbox. Settings are saved in ", h("code", null, state.config_path), "."),
    state.selected ? h("p", { class: "notice" }, "maggot ui was started with --toolbox, so only that toolbox is shown.") : null,
    state.toolboxes.length ? h("table", { class: "tb-table" },
      h("thead", null, h("tr", null, h("th", null, "Name"), h("th", null, "Folder"), h("th", null, "Tools"), h("th", { "aria-label": "Actions" }))),
      h("tbody", null, rows)) : null,
    h("p", { style: "margin-top:18px" }, h("button", { class: "btn green", type: "button", onclick: addToolbox, disabled: state.selected }, "Add toolbox…")));
}

// ---------- import ----------

function importDialog() {
  const fileInput = h("input", { type: "file", accept: ".maggot,.zip", class: "hidden" });
  fileInput.addEventListener("change", () => { if (fileInput.files[0]) { close(); stageImport(fileInput.files[0]); } });
  const { close } = openDialog({
    title: "Import a tool",
    body: h("div", null,
      h("p", { style: "margin-top:0" }, "Choose a .maggot archive, or a tool folder (one with a tool.json). You can also drop a .maggot file anywhere on this page."),
      fileInput,
      h("div", { class: "row" },
        h("button", { class: "btn", type: "button", onclick: () => fileInput.click() }, "Choose archive…"),
        h("button", { class: "btn", type: "button", onclick: async () => { const p = await pick({ mode: "dir", title: "Choose a tool folder" }); if (p) { close(); stageImport(p); } } }, "Choose tool folder…"))),
    actions: [{ label: "Cancel", class: "quiet" }],
  });
}

async function stageImport(src, draft) {
  let staged;
  try {
    if (typeof src === "string") staged = await post("/api/import", { path: src });
    else {
      const fd = new FormData();
      fd.append("file", src, src.name);
      staged = await api("POST", "/api/import", fd);
    }
  } catch (err) { return toast(err.message, true); }
  const m = staged.preview.manifest;
  const removeDraft = h("input", { type: "checkbox", checked: true });
  const action = { install: "Install", upgrade: "Upgrade", downgrade: "Downgrade", reinstall: "Reinstall" }[staged.preview.action];
  let committed = false;
  openDialog({
    title: `${action} ${m.name}?`,
    body: h("div", null,
      h("dl", { class: "kv" },
        h("dt", null, "Tool"), h("dd", null, `${m.name} (${m.id})`),
        h("dt", null, "Version"), h("dd", null, m.version + (staged.preview.existing ? `, installed: ${staged.preview.existing}` : "")),
        h("dt", null, "Toolbox"), h("dd", null, staged.toolbox),
        h("dt", null, "Runs"), h("dd", null, h("code", null, [...(m.entry.interpreter || []), m.entry.bin || m.entry.path, ...(m.entry.args || [])].join(" "))),
        h("dt", null, "Needs"), h("dd", null, (m.requires || []).map((r) => r.bin).join(", ") || "nothing else"),
        h("dt", null, "Size"), h("dd", null, `${staged.preview.files} files, ${bytes(staged.preview.bytes)}`)),
      h("p", null, m.description),
      h("p", { class: "notice", style: "background:var(--sunk);color:var(--ink)" }, "This tool is code that runs on this computer. Import it only if you trust where it came from."),
      draft ? h("label", { class: "check" }, removeDraft, h("span", null, draft.kind === "draft" ? "Delete the draft after importing" : "Stop trying this folder after importing (the folder stays)")) : null),
    actions: [
      { label: "Cancel", class: "quiet" },
      { label: action, class: staged.needs_force ? "danger" : "green", onClick: async () => {
        try {
          const res = await post(`/api/import/${staged.token}`, { force: staged.needs_force });
          committed = true;
          if (draft && removeDraft.checked) await del(`/api/drafts?ref=${q(draft.ref)}`).catch(() => {});
          toast(`${{ Install: "Installed", Upgrade: "Upgraded", Downgrade: "Downgraded", Reinstall: "Reinstalled" }[action]} ${m.name}.`);
          await loadState();
          location.hash = "#/tool/" + encodeURIComponent(res.ref);
        } catch (err) { toast(err.message, true); }
      } },
    ],
    onClose: () => { if (!committed) del(`/api/import/${staged.token}`).catch(() => {}); },
  });
}

document.addEventListener("dragover", (e) => { if (e.dataTransfer.types.includes("Files")) e.preventDefault(); });
document.addEventListener("drop", (e) => {
  const f = e.dataTransfer.files[0];
  if (!f) return;
  e.preventDefault();
  if (/\.(maggot|zip)$/i.test(f.name)) stageImport(f);
  else toast("Drop files on a file field of a tool. Drop .maggot files anywhere to import them.");
});

// ---------- tool page ----------

async function toolPage(spec) {
  const [ref, query] = spec.split("?");
  let tool;
  try {
    tool = await get(`/api/tool?ref=${q(ref)}`);
  } catch (err) {
    clear(main, h("h1", { class: "page-title" }, "Tool not found"), h("p", null, err.message), h("a", { href: "#/" }, "Back to all tools"));
    return;
  }
  const m = tool.manifest;
  const saved = store("values:" + tool.ref);

  const draft = tool.draft ? { ...tool.draft, ref: tool.ref } : null;
  const params = new URLSearchParams(query || "");

  // ---------- header ----------
  const reqs = h("div", { class: "reqs", "aria-label": "Requirements" }, h("span", { class: "req wait" }, "Checking requirements…"));
  const deleteTool = draft ? null : h("button", { class: "btn", type: "button", onclick: async () => {
    if (!(await confirmDialog(`Delete ${m.name}?`, h("div", null, h("p", null, "This deletes the tool folder:"), h("p", null, h("code", null, tool.dir))), "Delete tool", true))) return;
    try { await del(`/api/tool?ref=${q(tool.ref)}`); toast(`Deleted ${m.name}.`); await loadState(); location.hash = "#/"; } catch (e) { toast(e.message, true); }
  } }, "Delete");
  // One line: breadcrumb, name, version, chips | actions. The description
  // is clamped to one line ("More" expands it); the Docs tab has it all.
  const desc = h("p", { class: "tp-desc", title: m.description }, m.description);
  const more = h("button", { class: "tp-more", type: "button", hidden: true }, "More");
  more.addEventListener("click", () => {
    const open = desc.classList.toggle("open");
    more.textContent = open ? "Less" : "More";
  });
  const header = h("header", { class: "tp-head" + (draft ? " draft" : "") },
    h("div", { class: "tp-titlebar" },
      h("div", { class: "tp-title" },
        h("nav", { class: "tp-crumbs", "aria-label": "Breadcrumb" },
          h("a", { href: "#/" }, "Tools"), h("span", { "aria-hidden": "true" }, "/"), h("span", null, draft ? "Drafts" : tool.toolbox), h("span", { "aria-hidden": "true" }, "/")),
        h("h1", null, m.name),
        h("span", { class: "tp-version" }, "v" + m.version),
        draft ? h("span", { class: "tp-draft-badge" }, "Draft") : null,
        reqs),
      h("div", { class: "tp-actions" },
        h("button", { class: "btn", type: "button", onclick: () => openTool(tool) }, "Show folder"),
        h("a", { class: "btn", href: `/api/export?ref=${q(tool.ref)}` }, "Export"),
        deleteTool)),
    h("div", { class: "tp-desc-row" }, desc, more),
    draft ? draftNote(draft, m) : null);
  // Show "More" only when the description does not fit on one line.
  requestAnimationFrame(() => { more.hidden = desc.scrollWidth <= desc.clientWidth + 1; });

  // ---------- run tab: form | result ----------
  const warmBox = m.warm ? h("div", { class: "warm-box" }) : null;
  let warmTimer = null;
  if (warmBox) {
    renderWarm(tool, warmBox, tool.warm);
    warmTimer = setInterval(async () => {
      if (warmBox.contains(document.activeElement)) return; // do not redraw while the user types
      try { renderWarm(tool, warmBox, await get(`/api/tool/warm?ref=${q(tool.ref)}`)); } catch { /* page left */ }
    }, 4000);
  }

  const cmd = h("pre", { class: "cmd", "aria-live": "polite", title: "The command maggot runs" });
  const runBtn = h("button", { class: "run-btn", type: "submit" }, "Run");
  const skip = h("label", { class: "check", style: "font-size:13px" }, h("input", { type: "checkbox" }), h("span", null, "Skip requirement check"));
  skip.hidden = true;
  // Where files go: the run folder (review, then move) or a folder the user picks.
  const writesFiles = !m.outputs || !["stdout", "none", "inplace"].includes(m.outputs.kind);
  let out = store("out:" + tool.ref) || { mode: "run", path: "" };
  const outDir = () => (out.mode === "folder" && out.path ? out.path : "");
  const outControl = writesFiles ? h("div", { class: "field out-field" }) : null;
  function renderOut() {
    if (!outControl) return;
    const name = "out-" + m.id;
    const radio = (mode, label) => {
      const input = h("input", { type: "radio", name, value: mode, checked: out.mode === mode });
      input.addEventListener("change", async () => {
        if (mode === "folder" && !out.path) {
          const p = await pick({ mode: "dir", title: "Save files to this folder" });
          if (!p) { renderOut(); return; }
          out.path = p;
        }
        out.mode = mode;
        store("out:" + tool.ref, out);
        renderOut();
        preview();
      });
      return h("label", null, input, h("span", null, label));
    };
    const change = h("button", { class: "btn small quiet", type: "button" }, "Change…");
    change.addEventListener("click", async () => {
      const p = await pick({ mode: "dir", title: "Save files to this folder", start: out.path || undefined });
      if (p) { out = { mode: "folder", path: p }; store("out:" + tool.ref, out); renderOut(); preview(); }
    });
    clear(outControl,
      h("div", { class: "label", id: name + "-label" }, "Save files to"),
      h("div", { class: "segmented", role: "radiogroup", "aria-labelledby": name + "-label" },
        radio("run", "Run folder"), radio("folder", "A folder I choose")),
      h("div", { class: "help" }, out.mode === "folder"
        ? [h("code", null, out.path), " ", change]
        : "Files stay in maggot's run folder. Look at them, then move them where you want."));
  }
  renderOut();

  const preview = debounce(async () => {
    const values = form.values();
    store("values:" + tool.ref, values);
    try {
      const res = await post("/api/preview", { ref: tool.ref, values, out_dir: outDir() });
      // Only required fields missing: a hint, not an error.
      const lines = (res.error || "").split("\n").slice(1).map((l) => l.trim().replace(/^- /, ""));
      const onlyRequired = res.error && lines.length && lines.every((l) => l.endsWith(": is required"));
      cmd.classList.toggle("err", !!res.error && !onlyRequired);
      cmd.classList.toggle("todo", !!onlyRequired);
      if (onlyRequired) {
        const names = lines.map((l) => l.slice(0, -": is required".length));
        const labels = names.map((n) => { const i = (m.inputs || []).find((x) => x.name === n); return i && i.label ? i.label : n.replace(/_/g, " "); });
        cmd.textContent = "Fill in " + labels.join(" and ") + " to see the command.";
      } else {
        cmd.textContent = res.error || res.command;
      }
    } catch (err) { cmd.classList.add("err"); cmd.textContent = err.message; }
  }, 120);
  const form = buildForm(m.inputs || [], saved, preview);
  const options = (outControl || warmBox)
    ? h("div", { class: "fieldset tp-options" }, h("h3", { class: "group-title" }, "Options"), outControl, warmBox ? h("div", { class: "field" }, warmBox) : null)
    : null;
  if (options) form.el.append(options);
  const formEl = h("form", { class: "form tp-form", novalidate: true, "aria-label": "Inputs" },
    form.el,
    h("div", { class: "tp-dock" },
      cmd,
      h("div", { class: "go" }, runBtn, skip,
        h("button", { class: "btn quiet", type: "button", onclick: () => form.set(null) }, "Reset"))));
  preview();

  const runView = new RunView({
    onFinish: (meta) => { loadHistory(); toast(meta.status === "succeeded" ? `${m.name} finished.` : `${m.name}: ${meta.status}.`, meta.status !== "succeeded"); },
    onReuse: (values) => { form.set(values); toast("Form filled from this run."); },
    onTaken: () => loadHistory(),
  });

  formEl.addEventListener("submit", async (e) => {
    e.preventDefault();
    runBtn.disabled = true;
    try {
      const wp = m.warm ? warmPref(tool) : null;
      const meta = await post("/api/runs", {
        ref: tool.ref, values: form.values(), skip_checks: skip.querySelector("input").checked, out_dir: outDir(),
        keep_warm: wp && wp.on ? wp.minutes : 0,
      });
      await runView.show(meta.id);
      loadHistory();
    } catch (err) {
      toast(err.message, true);
      if (err.status === 409 && err.data.missing) skip.hidden = false;
    } finally { runBtn.disabled = false; }
  });

  // ---------- history tab: list | selected run ----------
  const historyList = h("ul", { class: "history" });
  const historyView = new RunView({
    emptyText: "Pick a run on the left to see its output and files.",
    onReuse: (values) => { form.set(values); select("run"); toast("Form filled from this run."); },
    onTaken: () => loadHistory(),
  });
  const deleteAll = h("button", { class: "btn small quiet danger", type: "button" }, "Delete all");
  deleteAll.addEventListener("click", async () => {
    if (!(await confirmDialog(`Delete all runs of ${m.name}?`,
      "This deletes every finished run of this tool and its output files. Runs still in progress are kept.", "Delete all", true))) return;
    try {
      const res = await del(`/api/runs?tool=${q(m.id)}`);
      historyView.idle();
      if (runView.meta && runView.meta.status !== "running") runView.idle();
      toast(`Deleted ${res.deleted} run${res.deleted === 1 ? "" : "s"}.`);
      loadHistory();
    } catch (err) { toast(err.message, true); }
  });
  let historyActive = params.get("run");
  async function loadHistory() {
    const { runs } = await get(`/api/runs?tool=${q(m.id)}&limit=50`);
    countBadge.textContent = runs.length ? String(runs.length) : "";
    deleteAll.hidden = !runs.length;
    clear(historyList, runs.length ? runs.map((r) => {
      const running = r.status === "running";
      const remove = h("button", {
        class: "btn small quiet danger", type: "button", disabled: running,
        title: running ? "Cancel the run before deleting it" : null,
        "aria-label": `Delete run from ${ago(r.started)}`,
      }, "Delete");
      remove.addEventListener("click", async () => {
        if (!(await confirmDialog("Delete this run?", "This deletes the run and its output files.", "Delete", true))) return;
        try {
          await del(`/api/runs/${r.id}`);
          if (historyView.id === r.id) historyView.idle();
          if (runView.id === r.id) runView.idle();
          loadHistory();
        } catch (err) { toast(err.message, true); }
      });
      return h("li", null,
        h("button", {
          class: "pick", type: "button", "aria-current": String(r.id === historyActive),
          onclick: () => { historyActive = r.id; historyView.show(r.id); loadHistory(); setURL({ run: r.id }); },
        }, h("span", { class: "status " + r.status }), h("span", null, ago(r.started)), h("span", { class: "when" }, duration(r.started, r.ended))),
        remove);
    }) : h("li", { class: "empty-note" }, "No runs yet."));
    return runs;
  }

  // ---------- code and docs tabs ----------
  const files = h("div", { class: "code-browser" });
  const docs = h("div", { class: "tp-docs" },
    h("dl", { class: "kv" },
      h("dt", null, "Folder"), h("dd", null, h("code", null, tool.dir)),
      h("dt", null, "Without maggot"), h("dd", null, h("code", null, tool.direct + " …")),
      h("dt", null, "From an agent"), h("dd", null, h("code", null, `maggot run ${tool.ref} -- …`))),
    tool.readme ? renderMarkdown(tool.readme) : h("p", { class: "muted" }, "This tool has no README."));
  const secrets = h("div", { class: "tp-settings" });
  const hasSecrets = (tool.env || []).some((e) => e.secret);

  // ---------- tabs ----------
  const countBadge = h("span", { class: "tab-count" });
  const tabDefs = [
    ["run", "Run"], ["history", ["History", countBadge]], ["code", "Code"], ["docs", "Docs"],
    hasSecrets ? ["settings", "Settings"] : null,
  ].filter(Boolean);
  const panels = {
    run: h("div", { class: "tp-split" }, formEl, h("div", { class: "tp-result" }, runView.el)),
    history: h("div", { class: "tp-split tp-split-list" },
      h("div", { class: "tp-list" }, h("div", { class: "tp-list-head" }, h("h2", null, "Runs"), deleteAll), historyList),
      h("div", { class: "tp-result" }, historyView.el)),
    code: files,
    docs,
    settings: secrets,
  };
  const tabButtons = {};
  const tabBar = h("div", { class: "tp-tabs", role: "tablist", "aria-label": "Tool sections" },
    tabDefs.map(([id, label]) => {
      const b = h("button", { type: "button", role: "tab", id: "tab-" + id, "aria-controls": "panel-" + id }, label);
      b.addEventListener("click", () => select(id));
      tabButtons[id] = b;
      return b;
    }));
  tabBar.addEventListener("keydown", (e) => {
    const ids = tabDefs.map(([id]) => id);
    const i = ids.indexOf(current);
    if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
      e.preventDefault();
      const next = ids[(i + (e.key === "ArrowRight" ? 1 : ids.length - 1)) % ids.length];
      select(next);
      tabButtons[next].focus();
    }
  });
  const panelEls = {};
  for (const [id] of tabDefs) {
    panelEls[id] = h("section", { class: "tp-panel tp-panel-" + id, role: "tabpanel", id: "panel-" + id, "aria-labelledby": "tab-" + id, hidden: true }, panels[id]);
  }
  let current = null;
  const loaded = {};
  function setURL(extra) {
    const p = new URLSearchParams();
    if (current && current !== "run") p.set("tab", current);
    for (const [k, v] of Object.entries(extra || {})) if (v) p.set(k, v);
    const qs = p.toString();
    history.replaceState(null, "", "#/tool/" + encodeURIComponent(tool.ref) + (qs ? "?" + qs : ""));
  }
  function select(id) {
    if (!panelEls[id]) id = "run";
    current = id;
    for (const [tid, b] of Object.entries(tabButtons)) {
      const on = tid === id;
      b.setAttribute("aria-selected", String(on));
      b.tabIndex = on ? 0 : -1;
      panelEls[tid].hidden = !on;
    }
    if (id === "code" && !loaded.code) { loaded.code = true; loadFiles(tool, files, params.get("file")); }
    if (id === "settings" && !loaded.settings) { loaded.settings = true; renderSecrets(tool, secrets); }
    setURL(id === "history" && historyActive ? { run: historyActive } : null);
  }

  main.classList.add("tool-main");
  clear(main, h("div", { class: "tp" }, header, tabBar, Object.values(panelEls)));
  page = { cleanup: () => { runView.stop(); historyView.stop(); clearInterval(warmTimer); main.classList.remove("tool-main"); } };

  const startTab = params.get("tab") || (params.has("code") ? "code" : params.get("run") ? "history" : "run");
  select(startTab);
  const runs = await loadHistory();
  // The result pane opens on the latest run, so the last result is right there.
  if (runs.length) runView.show(runs[0].id).catch(() => {});
  if (historyActive) historyView.show(historyActive).catch(() => {});
  else if (runs.length) { historyActive = runs[0].id; historyView.show(runs[0].id).catch(() => {}); loadHistory(); }
  checkHealth(tool, reqs);
}

// Warm control: "Keep in memory between runs for N min". Checking it does
// not load anything; the next Run loads the tool once and keeps it. The
// countdown starts after each run. Unchecking stops it and frees the memory.
function warmPref(tool) {
  const p = store("warm:" + tool.ref) || {};
  return { on: !!p.on, minutes: p.minutes > 0 ? p.minutes : 10 };
}

function renderWarm(tool, el, info) {
  const st = info && info.status;
  const pref = warmPref(tool);
  // No saved choice yet: show what the running process was started with.
  if (!store("warm:" + tool.ref) && st && st.idle) pref.minutes = Math.max(1, Math.round(st.idle / 6e10));
  const box = h("input", { type: "checkbox", checked: pref.on || !!st });
  const mins = h("input", {
    class: "input mins", type: "number", min: "1", step: "1", value: String(pref.minutes),
    "aria-label": "Minutes to keep it in memory after a run",
  });
  const save = () => store("warm:" + tool.ref, { on: box.checked, minutes: Math.max(1, Number(mins.value) || 10) });
  mins.addEventListener("change", () => { if (Number(mins.value) < 1) mins.value = "1"; save(); });
  box.addEventListener("change", async () => {
    save();
    if (!box.checked && st) {
      try { renderWarm(tool, el, await del(`/api/tool/warm?ref=${q(tool.ref)}`)); toast("Stopped. Memory is free again."); }
      catch (e) { toast(e.message, true); }
      return;
    }
    renderWarm(tool, el, info);
  });

  let status;
  if (st && st.running) {
    const left = Math.max(1, Math.round(st.idle_left / 60));
    status = h("span", { class: "req warm-on" }, st.busy
      ? "In memory, running a job"
      : `In memory: stops in ${left} min without a run (${st.jobs} run${st.jobs === 1 ? "" : "s"} so far)`);
  } else if (st) {
    status = h("span", { class: "req wait" }, "Loading into memory…");
  } else {
    status = h("span", { class: "muted" }, box.checked ? "Not loaded yet: the next run loads it and keeps it." : "Loads on every run.");
  }
  clear(el,
    h("label", { class: "check warm-check" }, box, h("span", null, "Keep in memory between runs for")),
    mins, h("span", null, "min"),
    status);
}

async function openTool(tool) {
  try { await post("/api/tool/open", { ref: tool.ref }); } catch (e) { toast(e.message, true); }
}

const LANG_NAMES = { json: "JSON", python: "Python", shell: "Shell", js: "JavaScript", go: "Go", yaml: "YAML", toml: "TOML", markdown: "Markdown" };

// loadFiles shows the tool's files (read-only) with a code view.
async function loadFiles(tool, el, startPath) {
  let res;
  try { res = await get(`/api/tool/files?ref=${q(tool.ref)}`); } catch (e) { return toast(e.message, true); }
  const view = h("div", { class: "code-view" });
  const list = h("ul", { class: "file-list", "aria-label": "Files" });
  let current = null;
  async function show(f, btn) {
    if (current) current.removeAttribute("aria-current");
    btn.setAttribute("aria-current", "true");
    current = btn;
    clear(view, h("p", { class: "muted" }, "Loading…"));
    let file;
    try { file = await get(`/api/tool/file?ref=${q(tool.ref)}&path=${q(f.path)}`); } catch (e) { return clear(view, h("p", { class: "notice" }, e.message)); }
    if (file.binary) return clear(view, h("div", { class: "code-head" }, h("code", null, f.path), h("span", { class: "muted" }, bytes(file.size))), h("p", { class: "muted", style: "padding:12px" }, "Binary file, not shown."));
    const text = file.text.replace(/\n$/, "");
    const lang = language(f.path, text);
    const lines = highlightLines(text, lang);
    clear(view,
      h("div", { class: "code-head" }, h("code", null, f.path),
        h("span", { class: "muted" }, [lang ? LANG_NAMES[lang] : null, `${lines.length} lines`, bytes(file.size)].filter(Boolean).join(" · "))),
      file.truncated ? h("p", { class: "notice", style: "margin:8px" }, "Only the first 512 KB is shown.") : null,
      h("pre", { class: "code", tabindex: "0" }, lines.map((l) => h("span", { class: "ln" }, l, "\n"))));
  }
  const items = res.files.map((f) => {
    if (f.skipped) return h("li", null, h("span", { class: "skipped", title: "Not listed" }, f.path));
    const btn = h("button", { type: "button" }, h("span", null, f.path), h("span", { class: "size" }, bytes(f.size)));
    btn.addEventListener("click", () => show(f, btn));
    return h("li", null, btn);
  });
  clear(list, items);
  clear(el, list, view);
  const wanted = startPath && [...list.querySelectorAll("button")].find((b) => b.firstChild.textContent === startPath);
  const first = wanted || list.querySelector("button");
  if (first) first.click();
}

function draftNote(draft, m) {
  const isDraftsFolder = draft.kind === "draft";
  const remove = h("button", { class: "btn small quiet danger", type: "button" }, isDraftsFolder ? "Delete draft" : "Stop trying");
  remove.addEventListener("click", async () => {
    if (isDraftsFolder && !(await confirmDialog(`Delete the draft of ${m.name}?`,
      h("div", null, h("p", null, "This deletes the draft folder:"), h("p", null, h("code", null, draft.path))), "Delete draft", true))) return;
    try { await del(`/api/drafts?ref=${q(draft.ref)}`); await loadState(); location.hash = "#/"; } catch (e) { toast(e.message, true); }
  });
  return h("div", { class: "draft-note" },
    h("p", null, "Not in your toolbox yet. Try it here, then import it.", h("br"), h("code", null, draft.path)),
    h("div", { class: "row" },
      h("button", { class: "btn green", type: "button", onclick: () => stageImport(draft.path, draft) }, "Import…"),
      remove));
}

async function checkHealth(tool, el) {
  let health;
  try { health = await get(`/api/tool/health?ref=${q(tool.ref)}`); } catch (err) { clear(el, h("span", { class: "req bad" }, err.message)); return; }
  const chips = health.requirements.map((r) => {
    if (r.status === "ok") return h("span", { class: "req", title: r.path }, r.bin + (r.version ? " " + r.version : ""));
    const label = r.status === "missing" ? `${r.bin} missing` : r.status === "version_mismatch" ? `${r.bin} ${r.version}, needs ${r.constraint}` : `${r.bin}: version unknown`;
    const chip = h("span", { class: "req bad" }, label);
    if (!r.install) return [chip, Object.keys(r.alternatives || {}).length ? h("span", { class: "muted", style: "font-size:13px" }, "Install by hand: " + Object.values(r.alternatives)[0]) : null];
    const cmd = r.install.command.join(" ");
    const btn = h("button", { class: "btn small", type: "button" }, "Install");
    btn.addEventListener("click", async () => {
      if (cmd.startsWith("sudo ")) { navigator.clipboard.writeText(cmd); return toast("This needs sudo. The command is copied; run it in a terminal."); }
      if (!(await confirmDialog(`Install ${r.bin}?`, h("div", null, h("p", null, "maggot will run:"), h("pre", { class: "cmd" }, cmd),
        h("p", null, `${r.install.manager} may also upgrade other installed packages, which can affect other programs on this computer.`)), "Install"))) return;
      btn.disabled = true; btn.textContent = "Installing…";
      try {
        const res = await post("/api/tool/install", { ref: tool.ref, bin: r.bin });
        if (res.ok) toast(`Installed ${r.bin}.`);
        else openDialog({ title: `Install of ${r.bin} failed`, body: h("pre", { class: "log", style: "height:auto;max-height:50vh" }, res.output || res.error), actions: [{ label: "Close" }] });
      } catch (err) { toast(err.message, true); }
      checkHealth(tool, el);
    });
    return [chip, btn];
  });
  const missingEnv = health.env.filter((e) => e.required && !e.set && !e.secret).map((e) => h("span", { class: "req bad" }, `env ${e.name} not set`));
  clear(el, tool.draft ? null : testChip(tool), chips, missingEnv, !chips.length && !missingEnv.length ? h("span", { class: "req" }, "No requirements") : null);
}

// testChip shows the last `maggot test` result, so broken tools stand out.
function testChip(tool) {
  const t = tool.last_test;
  const hint = `Run: maggot test ${tool.ref}`;
  if (!t) return h("span", { class: "req wait", title: hint }, "Not tested");
  if (t.version !== tool.manifest.version) return h("span", { class: "req wait", title: hint }, `Tested v${t.version} only`);
  if (t.ok) return h("span", { class: "req", title: `${t.passed} passed, ${t.skipped} skipped` }, `Tests passed ${ago(t.at)}`);
  return h("span", { class: "req bad", title: t.error || `${t.failed} failed. ${hint}` }, `Tests failed ${ago(t.at)}`);
}

function renderSecrets(tool, el) {
  const secrets = (tool.env || []).filter((e) => e.secret);
  if (!secrets.length) return clear(el);
  clear(el, h("div", { class: "panel" },
    h("div", { class: "panel-head" }, h("h2", null, "Secrets")),
    h("div", { class: "panel-body" },
      h("p", { class: "muted", style: "margin:0 0 10px;font-size:13.5px" }, "Saved in your system keychain, never in the toolbox."),
      secrets.map((s) => {
        const box = h("input", { class: "input mono", type: "password", autocomplete: "off", "aria-label": s.name, placeholder: s.set ? "Saved. Type to replace." : "Not set" });
        const save = h("button", { class: "btn small", type: "button" }, "Save");
        save.addEventListener("click", async () => {
          try { const res = await post("/api/tool/secret", { ref: tool.ref, name: s.name, value: box.value }); tool.env = res.env; toast(`Saved ${s.name}.`); renderSecrets(tool, el); } catch (e) { toast(e.message, true); }
        });
        const clr = s.source === "store" ? h("button", { class: "btn small quiet danger", type: "button", onclick: async () => {
          try { const res = await del(`/api/tool/secret?ref=${q(tool.ref)}&name=${q(s.name)}`); tool.env = res.env; renderSecrets(tool, el); } catch (e) { toast(e.message, true); }
        } }, "Clear") : null;
        return h("div", { class: "field" },
          h("div", { class: "label" }, h("code", null, s.name), s.required ? h("span", { class: "req-mark" }, "required") : null,
            s.source === "env" ? h("span", { class: "req-mark" }, "set in the environment") : null),
          h("div", { class: "row" }, box, save, clr),
          s.description ? h("div", { class: "help" }, s.description) : null);
      }))));
}

// ---------- start ----------

loadState().then(route).catch((err) => {
  clear(main, h("h1", { class: "page-title" }, "maggot could not start"), h("p", { class: "notice" }, err.message));
});
