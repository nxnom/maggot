// JSON API client. Errors carry the server message and extra fields.

export async function api(method, path, body) {
  const init = { method, headers: {} };
  if (body instanceof FormData) init.body = body;
  else if (body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const res = await fetch(path, init);
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { error: text }; }
  if (!res.ok) {
    const err = new Error((data && data.error) || res.statusText);
    err.status = res.status;
    err.data = data || {};
    throw err;
  }
  return data;
}

export const get = (p) => api("GET", p);
export const post = (p, b) => api("POST", p, b === undefined ? {} : b);
export const del = (p) => api("DELETE", p);
export const q = encodeURIComponent;

export async function upload(files) {
  const fd = new FormData();
  for (const f of files) fd.append("files", f, f.name);
  const res = await api("POST", "/api/uploads", fd);
  return res.paths;
}

export function fileURL(runId, path, download = false) {
  const enc = path.split("/").map(encodeURIComponent).join("/");
  return `/api/runs/${runId}/files/${enc}` + (download ? "?download=1" : "");
}
