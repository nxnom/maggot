// A folder/file picker that browses this computer through the local server,
// so the tool gets real paths (no copying of large files).
import { h, clear, openDialog, bytes, store, toast } from "./dom.js";
import { get, q } from "./api.js";

// mode: "dir" (choose a folder), "file" (one file), "files" (several files).
export function pick({ mode = "dir", title, start }) {
  return new Promise((resolve) => {
    let result = null;
    let current = start || store("picker:last") || "";
    let showHidden = false;
    const selected = new Set();

    const pathInput = h("input", { class: "input", "aria-label": "Folder path", spellcheck: "false" });
    const list = h("ul", { class: "picker-list", role: "list" });
    const info = h("p", { class: "muted", style: "margin:8px 0 0;font-size:13px" });
    const hiddenToggle = h("label", { class: "check", style: "margin-top:8px" },
      h("input", { type: "checkbox", onchange: (e) => { showHidden = e.target.checked; load(current); } }),
      h("span", null, "Show hidden files"));

    const body = h("div", null,
      h("div", { class: "picker-path" },
        h("button", { class: "btn small", type: "button", onclick: () => up(), "aria-label": "Parent folder" }, "Up"),
        pathInput,
        h("button", { class: "btn small", type: "button", onclick: () => load(pathInput.value) }, "Go")),
      list, info, hiddenToggle);
    pathInput.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); load(pathInput.value); } });

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

    function up() { if (parent) load(parent); }

    async function load(path) {
      try {
        const res = await get(`/api/fs?path=${q(path || "")}&files=${mode === "dir" ? 0 : 1}&hidden=${showHidden ? 1 : 0}`);
        current = res.path;
        parent = res.parent;
        pathInput.value = current;
        render(res);
      } catch (err) {
        toast(err.message, true);
      }
    }

    function render(res) {
      const rows = res.entries.map((e) => {
        const isSel = selected.has(e.path);
        const btn = h("button", { type: "button", "aria-pressed": e.dir ? null : String(isSel) },
          h("span", { class: "icon", "aria-hidden": "true" }, e.dir ? "▸" : "·"),
          h("span", null, e.name),
          e.toolbox ? h("span", { class: "tag" }, "toolbox") : null,
          e.dir ? null : h("span", { class: "size" }, bytes(e.size)));
        btn.addEventListener("click", () => {
          if (e.dir) return load(e.path);
          if (mode === "file") selected.clear();
          if (selected.has(e.path)) selected.delete(e.path); else selected.add(e.path);
          chooseBtn.textContent = chooseLabel();
          render(res);
        });
        return h("li", null, btn);
      });
      clear(list, rows.length ? rows : h("li", null, h("p", { class: "muted", style: "padding:8px 10px;margin:0" }, "This folder is empty.")));
      info.textContent = res.toolbox ? "This folder is a toolbox." : mode !== "dir" && selected.size ? `${selected.size} selected` : "";
    }

    load(current);
  });
}
