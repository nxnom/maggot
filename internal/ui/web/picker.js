// A folder/file picker that browses this computer through the local server,
// so the tool gets real paths (no copying of large files).
import { h, clear, openDialog, bytes, store, toast, debounce } from "./dom.js";
import { get, q } from "./api.js";

// mode: "dir" (choose a folder), "file" (one file), "files" (several files).
// accept: the input's file types (".json", "audio/*"); other files are hidden
// until the user turns on "Show all files".
export function pick({ mode = "dir", title, start, accept }) {
  return new Promise((resolve) => {
    let result = null;
    let current = start || store("picker:last") || "";
    let showHidden = false;
    let showAll = false;
    const types = (accept || []).filter(Boolean);
    const selected = new Set();
    const acceptParam = () => (types.length && !showAll ? "&accept=" + q(types.join(",")) : "");

    const search = h("input", {
      class: "input picker-search", type: "search", spellcheck: "false",
      placeholder: mode === "dir" ? "Search folders in here and below" : "Search files in here and below",
      "aria-label": "Search this folder and its subfolders",
    });
    const filterNote = h("p", { class: "muted picker-filter", hidden: !types.length });
    const allToggle = types.length ? h("label", { class: "check" },
      h("input", { type: "checkbox", onchange: (e) => { showAll = e.target.checked; refresh(); } }),
      h("span", null, "Show all files")) : null;

    const pathInput = h("input", { class: "input", "aria-label": "Folder path", spellcheck: "false" });
    const list = h("ul", { class: "picker-list", role: "list" });
    const info = h("p", { class: "muted", style: "margin:8px 0 0;font-size:13px" });
    const hiddenToggle = h("label", { class: "check" },
      h("input", { type: "checkbox", onchange: (e) => { showHidden = e.target.checked; refresh(); } }),
      h("span", null, "Show hidden files"));

    const body = h("div", null,
      h("div", { class: "picker-path" },
        h("button", { class: "btn small", type: "button", onclick: () => up(), "aria-label": "Parent folder" }, "Up"),
        pathInput,
        h("button", { class: "btn small", type: "button", onclick: () => go(pathInput.value) }, "Go")),
      search, filterNote, list, info, h("div", { class: "row picker-opts" }, hiddenToggle, allToggle));
    search.addEventListener("input", debounce(() => refresh(), 250));
    search.addEventListener("keydown", (e) => { if (e.key === "Enter") e.preventDefault(); });
    pathInput.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); go(pathInput.value); } });

    let parent = "";
    const chooseLabel = () => mode === "dir" ? "Choose this folder" : selected.size > 1 ? `Choose ${selected.size} files` : "Choose file";
    const { foot } = openDialog({
      title: title || (mode === "dir" ? "Choose a folder" : "Choose files"),
      body,
      actions: [
        { label: "Cancel", class: "quiet" },
        {
          label: chooseLabel(), class: "green", onClick: () => {
            if (mode === "dir") result = current;
            else if (selected.size === 0) { toast("Select a file first.", true); return false; }
            else result = mode === "file" ? [...selected][0] : [...selected];
            store("picker:last", current);
          },
        },
      ],
      onClose: () => resolve(result),
    });
    const chooseBtn = foot.lastChild;

    // Moving to another folder ends a search.
    function go(path) { search.value = ""; return load(path); }
    function up() { if (parent) go(parent); }

    const filesParam = () => `&files=${mode === "dir" ? 0 : 1}&hidden=${showHidden ? 1 : 0}`;
    let lastRes = null;

    function refresh() { return search.value.trim() ? find() : load(current); }

    async function load(path) {
      try {
        const res = await get(`/api/fs?path=${q(path || "")}${filesParam()}${acceptParam()}`);
        current = res.path;
        parent = res.parent;
        store("picker:last", current); // the next picker opens here too
        pathInput.value = current;
        lastRes = res;
        if (types.length) {
          filterNote.textContent = showAll ? "Showing all files." : `Showing ${types.join(", ")} files` +
            (res.filtered ? ` · ${res.filtered} other file${res.filtered === 1 ? "" : "s"} hidden` : "");
        }
        render(res);
      } catch (err) {
        toast(err.message, true);
      }
    }

    let findSeq = 0;
    async function find() {
      const term = search.value.trim();
      const seq = ++findSeq;
      clear(list, h("li", null, h("p", { class: "muted", style: "padding:8px 10px;margin:0" }, "Searching…")));
      let res;
      try { res = await get(`/api/fs/find?path=${q(current)}&q=${q(term)}${filesParam()}${acceptParam()}`); } catch (err) { return toast(err.message, true); }
      if (seq !== findSeq || search.value.trim() !== term) return;
      lastRes = { ...res, search: true };
      render(lastRes);
    }

    function render(res) {
      const rows = res.entries.map((e) => {
        const isSel = selected.has(e.path);
        const btn = h("button", { type: "button", "aria-pressed": e.dir ? null : String(isSel), title: e.path },
          h("span", { class: "icon", "aria-hidden": "true" }, e.dir ? "▸" : "·"),
          res.search ? h("span", { class: "picker-rel" }, h("span", null, e.name), e.rel !== e.name ? h("span", { class: "muted" }, e.rel.slice(0, -e.name.length - 1)) : null)
            : h("span", null, e.name),
          e.toolbox ? h("span", { class: "tag" }, "toolbox") : null,
          e.dir ? null : h("span", { class: "size" }, bytes(e.size)));
        btn.addEventListener("click", () => {
          if (e.dir) { search.value = ""; return load(e.path); }
          if (mode === "file") selected.clear();
          if (selected.has(e.path)) selected.delete(e.path); else selected.add(e.path);
          chooseBtn.textContent = chooseLabel();
          render(lastRes);
        });
        return h("li", null, btn);
      });
      const empty = res.search ? `Nothing matches "${search.value.trim()}" in this folder or below.`
        : res.filtered ? "No matching files here. Search below, or turn on Show all files." : "This folder is empty.";
      clear(list, rows.length ? rows : h("li", null, h("p", { class: "muted", style: "padding:8px 10px;margin:0" }, empty)));
      const notes = [];
      if (res.search) notes.push(`${res.entries.length} found${res.partial ? " (search stopped early; type more to narrow it)" : ""}`);
      if (res.toolbox && !res.search) notes.push("This folder is a toolbox.");
      if (mode !== "dir" && selected.size) notes.push(`${selected.size} selected`);
      info.textContent = notes.join(" · ");
    }

    load(current);
  });
}
