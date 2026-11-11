// The run panel: live status, progress, output lines, result files.
import { h, clear, bytes, duration, toast, confirmDialog } from "./dom.js";
import { get, post, fileURL } from "./api.js";
import { pick } from "./picker.js";

const MAX_LINES = 2000;

export class RunView {
  constructor({ onFinish, onReuse, onTaken }) {
    this.onFinish = onFinish;
    this.onReuse = onReuse;
    this.onTaken = onTaken;
    this.source = null;
    this.el = h("section", { class: "panel", "aria-label": "Run" });
    this.idle();
  }

  idle() {
    this.stop();
    this.id = null;
    this.meta = null;
    clear(this.el,
      h("div", { class: "panel-head" }, h("h2", null, "Run")),
      h("div", { class: "panel-body" }, h("p", { class: "muted", style: "margin:0" },
        "Fill in the form and press Run. Output and result files show up here.")));
  }

  stop() {
    if (this.source) { this.source.close(); this.source = null; }
    clearInterval(this.timer);
  }

  async show(id) {
    this.stop();
    this.id = id;
    const status = h("span", { class: "status" });
    const time = h("span", { class: "muted", style: "font-size:13px" });
    const cancel = h("button", { class: "btn small", type: "button", hidden: true }, "Cancel");
    const reuse = h("button", { class: "btn small quiet", type: "button" }, "Use these values");
    const reveal = h("button", { class: "btn small quiet", type: "button" }, "Show folder");
    const meter = h("div", { class: "meter indeterminate", role: "progressbar", "aria-label": "Progress" }, h("div"));
    const log = h("pre", { class: "log", tabindex: "0", "aria-label": "Output" });
    const results = h("div");
    this.parts = { status, time, cancel, meter, log, results };
    clear(this.el,
      h("div", { class: "panel-head" }, h("h2", null, "Run"), status, time, cancel),
      h("div", { class: "panel-body" }, meter, log,
        h("div", { class: "row", style: "margin-top:10px;flex-wrap:wrap" }, reveal, reuse),
        results));

    cancel.addEventListener("click", async () => {
      cancel.disabled = true;
      try { await post(`/api/runs/${id}/cancel`); } catch (err) { toast(err.message, true); }
    });
    reveal.addEventListener("click", () => post("/api/reveal", { run: id }).catch((e) => toast(e.message, true)));

    const detail = await get(`/api/runs/${id}`);
    this.meta = detail.run;
    reuse.addEventListener("click", () => this.onReuse && this.onReuse(this.meta.values || {}));
    this.lines = 0;
    this.setStatus(this.meta);
    if (this.meta.status !== "running") {
      // Finished run: replay output, then show results.
      this.stream(id, true);
      return;
    }
    this.timer = setInterval(() => { time.textContent = duration(this.meta.started); }, 500);
    this.stream(id, false);
  }

  stream(id, finished) {
    const src = new EventSource(`/api/runs/${id}/stream`);
    this.source = src;
    const { log } = this.parts;
    let stick = true;
    log.addEventListener("scroll", () => { stick = log.scrollTop + log.clientHeight >= log.scrollHeight - 8; });
    src.onmessage = (e) => {
      const m = JSON.parse(e.data);
      if (m.type === "out" || m.type === "err") {
        this.lines++;
        log.append(m.type === "err" ? h("span", { class: "err" }, m.text + "\n") : m.text + "\n");
        while (log.childNodes.length > MAX_LINES) log.firstChild.remove();
        if (stick) log.scrollTop = log.scrollHeight;
      } else if (m.type === "progress") {
        this.setProgress(m.value);
      } else if (m.type === "status" && m.status) {
        this.meta = m.status;
        this.setStatus(m.status);
        if (m.status.status !== "running") {
          src.close();
          this.source = null;
          clearInterval(this.timer);
          this.showResults(id);
          if (!finished && this.onFinish) this.onFinish(m.status);
        }
      }
    };
    src.onerror = () => {
      if (this.source === src && src.readyState === EventSource.CLOSED) {
        this.source = null;
        this.showResults(id);
      }
    };
  }

  setProgress(v) {
    const { meter } = this.parts;
    meter.classList.remove("indeterminate");
    meter.firstChild.style.width = Math.round(v * 100) + "%";
    meter.setAttribute("aria-valuenow", String(Math.round(v * 100)));
  }

  setStatus(meta) {
    const { status, time, cancel, meter } = this.parts;
    const label = { running: "Running", succeeded: "Done", failed: "Failed", cancelled: "Cancelled" }[meta.status] || meta.status;
    status.className = "status " + meta.status;
    status.textContent = meta.status === "failed" && meta.exit_code !== undefined ? `Failed (exit ${meta.exit_code})` : label;
    cancel.hidden = meta.status !== "running" || meta.origin === "cli";
    time.textContent = duration(meta.started, meta.ended);
    if (meta.status !== "running") {
      if (meta.progress >= 0) this.setProgress(meta.status === "succeeded" ? 1 : meta.progress);
      else meter.classList.add("hidden");
    } else if (meta.progress >= 0) {
      this.setProgress(meta.progress);
    }
  }

  async showResults(id) {
    let detail;
    try { detail = await get(`/api/runs/${id}`); } catch { return; }
    if (this.id !== id) return;
    const { results } = this.parts;
    const items = [];
    if (detail.run.error) items.push(h("p", { class: "notice" }, detail.run.error));
    if (detail.stdout !== undefined && detail.stdout !== "") {
      let text = detail.stdout;
      if (detail.run.format === "json") {
        try { text = JSON.stringify(JSON.parse(text), null, 2); } catch { /* keep raw */ }
      }
      items.push(h("h3", { style: "font-size:14px;margin:16px 0 0" }, "Result"),
        h("pre", { class: "stdout mono" }, text),
        h("button", { class: "btn small", type: "button", style: "margin-top:8px", onclick: () => navigator.clipboard.writeText(text).then(() => toast("Copied.")) }, "Copy result"));
    }
    if (detail.outputs.length) {
      const n = detail.outputs.length;
      const move = h("button", { class: "btn small green", type: "button" }, "Move files to…");
      move.addEventListener("click", () => this.take(id, n));
      if (detail.run.review && detail.run.status === "succeeded") {
        items.push(h("div", { class: "review-note" },
          h("strong", null, "Waiting for your review. "),
          "The agent will move these files into place when you approve. To reject them, delete this run."));
      }
      items.push(h("div", { class: "row", style: "margin:16px 0 0;justify-content:space-between;flex-wrap:wrap" },
        h("h3", { style: "font-size:14px;margin:0" }, `${n} file${n === 1 ? "" : "s"}`), move),
        h("div", { class: "outputs" }, detail.outputs.slice(0, 60).map((f) => outputCard(id, f))));
    } else if (detail.run.status === "succeeded" && !items.length && this.lines === 0) {
      items.push(h("p", { class: "muted" }, "Finished with no output."));
    }
    clear(results, items);
  }
}

// take moves the run's files into a folder the user picks, then the run is gone.
RunView.prototype.take = async function (id, n) {
  const dest = await pick({ mode: "dir", title: "Move the files into this folder" });
  if (!dest) return;
  if (!(await confirmDialog(`Move ${n} file${n === 1 ? "" : "s"}?`,
    h("div", null, h("p", null, "The files are moved into:"), h("p", null, h("code", null, dest)),
      h("p", null, "The run is deleted afterwards, so no copy is left behind.")), "Move files"))) return;
  let res;
  try {
    res = await post(`/api/runs/${id}/take`, { dest });
  } catch (err) {
    if (err.status !== 409) return toast(err.message, true);
    const list = err.data.conflicts || [];
    if (!(await confirmDialog("Replace existing files?",
      h("div", null, h("p", null, "These files already exist in the folder:"), h("ul", null, list.map((f) => h("li", null, h("code", null, f))))),
      "Replace", true))) return;
    try { res = await post(`/api/runs/${id}/take`, { dest, overwrite: true }); } catch (e2) { return toast(e2.message, true); }
  }
  toast(`Moved ${res.moved.length} file${res.moved.length === 1 ? "" : "s"}.`);
  this.idle();
  if (this.onTaken) this.onTaken();
};

function outputCard(runId, f) {
  const url = fileURL(runId, f.path);
  const type = f.mime.split("/")[0];
  let media;
  if (type === "image" && f.mime !== "image/heic") media = h("img", { src: url, alt: f.path, loading: "lazy" });
  else if (type === "video") media = h("video", { src: url, controls: true, preload: "metadata" });
  else if (type === "audio") media = h("audio", { src: url, controls: true, preload: "none" });
  else media = h("span", { class: "ext", "aria-hidden": "true" }, (f.path.split(".").pop() || "file").slice(0, 5));
  return h("div", { class: "out-item" },
    h("div", { class: "media" }, media),
    h("div", { class: "meta" },
      h("div", { class: "name" }, f.path),
      h("div", { class: "muted" }, bytes(f.size)),
      h("div", { class: "links" },
        h("a", { class: "btn small", href: url, target: "_blank", rel: "noopener" }, "Open"),
        h("a", { class: "btn small", href: fileURL(runId, f.path, true) }, "Download"),
        h("button", { class: "btn small quiet", type: "button", onclick: () => post("/api/reveal", { run: runId, path: f.path }).catch((e) => toast(e.message, true)) }, "Show in folder"))));
}
