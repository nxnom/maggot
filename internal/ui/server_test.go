package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/nxnom/maggot/internal/presets"
	"github.com/nxnom/maggot/internal/scaffold"
	"github.com/nxnom/maggot/internal/secrets"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/warm"
)

type testServer struct {
	*httptest.Server
	s      *Server
	client *http.Client
	tb     string
	opened []string
}

// newTestServer starts a UI server with an isolated config, data dir and one
// toolbox holding the worker tool.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	t.Setenv("MAGGOT_CONFIG_DIR", t.TempDir())
	t.Setenv("MAGGOT_DATA_DIR", t.TempDir())
	t.Setenv("MAGGOT_SECRETS", "file")
	tool := makeTool(t, workerManifest, workerScript)
	tbDir := tool.Toolbox.Path
	if _, err := toolbox.Init(tbDir, "t"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tool.Dir, "tool.json"), []byte(workerManifest), 0o644)

	s, err := New("test", "")
	if err != nil {
		t.Fatal(err)
	}
	s.store = &secrets.FileStore{Path: filepath.Join(t.TempDir(), "s.json")}
	s.checker = doctor.New()
	ts := &testServer{s: s, tb: tbDir}
	s.open = func(p string, reveal bool) error { ts.opened = append(ts.opened, p); return nil }
	ts.Server = httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	s.AllowHost(u.Host)

	// Sign in like the browser does: open the token URL, keep the cookie.
	jar := &cookieJar{}
	ts.client = &http.Client{Jar: jar}
	resp, err := ts.client.Get(ts.URL + "/?token=" + s.Token)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("sign in: %v %v", resp, err)
	}
	resp.Body.Close()

	// Register the toolbox through the API.
	var added map[string]string
	ts.do(t, "POST", "/api/toolboxes", map[string]any{"path": tbDir}, 200, &added)
	return ts
}

type cookieJar struct{ cookies []*http.Cookie }

func (j *cookieJar) SetCookies(_ *url.URL, c []*http.Cookie) { j.cookies = append(j.cookies, c...) }
func (j *cookieJar) Cookies(_ *url.URL) []*http.Cookie       { return j.cookies }

func (ts *testServer) do(t *testing.T, method, path string, body any, want int, out any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	resp, err := ts.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
	}
}

func TestAuthAndHostChecks(t *testing.T) {
	ts := newTestServer(t)
	// No cookie.
	resp, _ := http.Get(ts.URL + "/api/state")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie: %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("index without cookie: %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/?token=wrong")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	// DNS rebinding: a foreign Host header is refused even with the cookie.
	req, _ := http.NewRequest("GET", ts.URL+"/api/state", nil)
	req.Host = "evil.example:80"
	resp, _ = ts.client.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host: %d", resp.StatusCode)
	}
	// Cross-origin write is refused.
	req, _ = http.NewRequest("POST", ts.URL+"/api/runs", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://evil.example")
	resp, _ = ts.client.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross origin: %d", resp.StatusCode)
	}
	// Signed in: app and assets load.
	resp, _ = ts.client.Get(ts.URL + "/")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("index: %d", resp.StatusCode)
	}
}

func TestTokenIsStable(t *testing.T) {
	t.Setenv("MAGGOT_CONFIG_DIR", t.TempDir())
	t.Setenv("MAGGOT_DATA_DIR", t.TempDir())
	t.Setenv("MAGGOT_SECRETS", "file")
	a, err := New("test", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := New("test", "")
	if a.Token != b.Token || len(a.Token) < 32 {
		t.Fatalf("token should persist: %q %q", a.Token, b.Token)
	}
	info, _ := os.Stat(filepath.Join(a.data, "ui-token"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v", info.Mode().Perm())
	}
}

func TestStateToolAndPreview(t *testing.T) {
	ts := newTestServer(t)
	var state struct {
		Toolboxes []struct {
			Name  string
			Tools int
		}
		Tools []toolSummary
	}
	ts.do(t, "GET", "/api/state", nil, 200, &state)
	if len(state.Toolboxes) != 1 || state.Toolboxes[0].Tools != 1 || state.Tools[0].Ref != "t/worker" {
		t.Fatalf("state: %+v", state)
	}
	var tool struct {
		Ref    string
		Direct string
	}
	ts.do(t, "GET", "/api/tool?ref=worker", nil, 200, &tool)
	if tool.Ref != "t/worker" || !strings.HasSuffix(tool.Direct, "/run") {
		t.Fatalf("tool: %+v", tool)
	}
	ts.do(t, "GET", "/api/tool?ref=nope", nil, 404, nil)

	var prev struct{ Command, Error string }
	ts.do(t, "POST", "/api/preview", map[string]any{"ref": "worker", "values": map[string]any{"name": "a b"}}, 200, &prev)
	if prev.Command != "./run --out '<run folder>' 'a b'" {
		t.Fatalf("preview: %+v", prev)
	}
	ts.do(t, "POST", "/api/preview", map[string]any{"ref": "worker", "values": map[string]any{}}, 200, &prev)
	if !strings.Contains(prev.Error, "name: is required") {
		t.Fatalf("preview error: %+v", prev)
	}
	var found struct{ Refs []string }
	ts.do(t, "GET", "/api/search?q=worker", nil, 200, &found)
	if len(found.Refs) != 1 {
		t.Fatalf("search: %+v", found)
	}
}

func TestRunOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	var meta RunMeta
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "worker", "values": map[string]any{"name": "ann"}}, 200, &meta)

	// Stream until the final status.
	resp, err := ts.client.Get(ts.URL + "/api/runs/" + meta.ID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	var final *RunMeta
	var lines []string
	deadline := time.After(10 * time.Second)
	for final == nil && sc.Scan() {
		select {
		case <-deadline:
			t.Fatal("stream timeout")
		default:
		}
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var m Message
		json.Unmarshal([]byte(line[6:]), &m)
		if m.Type == "out" {
			lines = append(lines, m.Text)
		}
		if m.Type == "status" && m.Status.Status != StatusRunning {
			final = m.Status
		}
	}
	resp.Body.Close()
	if final == nil || final.Status != StatusSucceeded || !strings.Contains(strings.Join(lines, "\n"), "cwd=") {
		t.Fatalf("final=%+v lines=%v", final, lines)
	}

	var detail struct {
		Run     RunMeta
		Outputs []OutputFile
	}
	ts.do(t, "GET", "/api/runs/"+meta.ID, nil, 200, &detail)
	if len(detail.Outputs) != 2 {
		t.Fatalf("outputs: %+v", detail.Outputs)
	}
	// Output file download, with path escape attempts refused.
	r2, _ := ts.client.Get(ts.URL + "/api/runs/" + meta.ID + "/files/ann.txt")
	body, _ := io.ReadAll(r2.Body)
	if r2.StatusCode != 200 || string(body) != "hello ann\n" {
		t.Fatalf("file: %d %q", r2.StatusCode, body)
	}
	os.Symlink("/etc/hosts", filepath.Join(final.OutDir, "link"))
	for _, bad := range []string{"../run.json", "link", "..%2frun.json"} {
		r3, _ := ts.client.Get(ts.URL + "/api/runs/" + meta.ID + "/files/" + bad)
		if r3.StatusCode == 200 {
			t.Errorf("%s should be refused", bad)
		}
	}
	// Replay of a finished run.
	r4, _ := ts.client.Get(ts.URL + "/api/runs/" + meta.ID + "/stream")
	replay, _ := io.ReadAll(r4.Body)
	if !strings.Contains(string(replay), "hello") && !strings.Contains(string(replay), "cwd=") {
		t.Fatalf("replay: %s", replay)
	}
	var list struct{ Runs []RunMeta }
	ts.do(t, "GET", "/api/runs?tool=worker", nil, 200, &list)
	if len(list.Runs) != 1 {
		t.Fatalf("runs: %+v", list)
	}
	ts.do(t, "POST", "/api/reveal", map[string]any{"run": meta.ID, "path": "ann.txt"}, 200, nil)
	if len(ts.opened) != 1 || !strings.HasSuffix(ts.opened[0], "ann.txt") {
		t.Fatalf("reveal: %v", ts.opened)
	}
	ts.do(t, "POST", "/api/reveal", map[string]any{"run": meta.ID, "path": "../../x"}, 404, nil)
	// Scriptable types are sandboxed; media, PDF and text are not (a sandbox
	// would break them in their own tab).
	for name, want := range map[string]string{"page.html": "sandbox", "pic.svg": "sandbox", "clip.mp4": "", "song.mp3": "", "photo.png": "", "notes.txt": ""} {
		os.WriteFile(filepath.Join(final.OutDir, name), []byte("x"), 0o644)
		rr, _ := ts.client.Get(ts.URL + "/api/runs/" + meta.ID + "/files/" + name)
		if got := rr.Header.Get("Content-Security-Policy"); got != want {
			t.Errorf("%s: CSP %q, want %q", name, got, want)
		}
		if rr.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: output files must not be cached", name)
		}
	}
	// PDFs are served without the sandbox so the browser's PDF viewer works.
	os.WriteFile(filepath.Join(final.OutDir, "doc.pdf"), []byte("%PDF-1.4"), 0o644)
	r5, _ := ts.client.Get(ts.URL + "/api/runs/" + meta.ID + "/files/doc.pdf")
	if r5.Header.Get("Content-Security-Policy") != "" || r5.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("pdf headers: %v", r5.Header)
	}
	ts.do(t, "DELETE", "/api/runs/"+meta.ID, nil, 200, nil)
	ts.do(t, "GET", "/api/runs/"+meta.ID, nil, 404, nil)

	// Delete all runs of a tool.
	for i := 0; i < 2; i++ {
		var m RunMeta
		ts.do(t, "POST", "/api/runs", map[string]any{"ref": "worker", "values": map[string]any{"name": "x"}}, 200, &m)
		r, _ := ts.s.runs.Active(m.ID)
		if r != nil {
			<-r.Done()
		}
		time.Sleep(1100 * time.Millisecond) // run ids are per second
	}
	var deleted struct{ Deleted int }
	ts.do(t, "DELETE", "/api/runs?tool=worker", nil, 200, &deleted)
	if deleted.Deleted != 2 {
		t.Fatalf("deleted %d", deleted.Deleted)
	}
	ts.do(t, "DELETE", "/api/runs", nil, 400, nil)
}

func TestRunPreflightAndValidation(t *testing.T) {
	ts := newTestServer(t)
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "worker", "values": map[string]any{}}, 400, nil)
	// A tool needing a missing binary is refused with a list of what is missing.
	m := `{"schema":1,"id":"needy","version":"1.0.0","name":"N","description":"d","entry":{"path":"run"},"requires":[{"bin":"no-such-bin-xyz"}]}`
	dir := filepath.Join(ts.tb, "tools", "needy")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "tool.json"), []byte(m), 0o644)
	os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o755)
	var res struct{ Missing []string }
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "needy"}, 409, &res)
	if len(res.Missing) != 1 {
		t.Fatalf("missing: %+v", res)
	}
	var health struct{ OK bool }
	ts.do(t, "GET", "/api/tool/health?ref=needy", nil, 200, &health)
	if health.OK {
		t.Fatal("health should not be ok")
	}
}

func TestImportUploadAndFS(t *testing.T) {
	ts := newTestServer(t)
	// Export the worker, remove it, import it back from the archive.
	r, _ := ts.client.Get(ts.URL + "/api/export?ref=worker")
	archive, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || len(archive) == 0 {
		t.Fatalf("export: %d", r.StatusCode)
	}
	ts.do(t, "DELETE", "/api/tool?ref=worker", nil, 200, nil)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "worker-1.0.0"+pack.Ext)
	fw.Write(archive)
	mw.Close()
	req, _ := http.NewRequest("POST", ts.URL+"/api/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, _ := ts.client.Do(req)
	var staged struct {
		Token      string
		NeedsForce bool `json:"needs_force"`
		Preview    struct{ Action string }
	}
	json.NewDecoder(resp.Body).Decode(&staged)
	if resp.StatusCode != 200 || staged.Preview.Action != "install" {
		t.Fatalf("stage: %d %+v", resp.StatusCode, staged)
	}
	var done struct{ Ref string }
	ts.do(t, "POST", "/api/import/"+staged.Token, map[string]any{}, 200, &done)
	if done.Ref != "t/worker" {
		t.Fatalf("import: %+v", done)
	}
	ts.do(t, "POST", "/api/import/"+staged.Token, map[string]any{}, 404, nil)

	// Upload two files with the same name.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	for i := 0; i < 2; i++ {
		fw, _ := mw.CreateFormFile("files", "../a.txt")
		fw.Write([]byte("x"))
	}
	mw.Close()
	req, _ = http.NewRequest("POST", ts.URL+"/api/uploads", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, _ = ts.client.Do(req)
	var up struct{ Paths []string }
	json.NewDecoder(resp.Body).Decode(&up)
	if len(up.Paths) != 2 || filepath.Base(up.Paths[0]) != "a.txt" || filepath.Base(up.Paths[1]) != "a-2.txt" ||
		!strings.HasPrefix(up.Paths[0], ts.s.data) {
		t.Fatalf("uploads: %+v", up)
	}

	var listing struct {
		Path    string
		Toolbox bool
		Entries []struct {
			Name    string
			Toolbox bool
		}
	}
	ts.do(t, "GET", "/api/fs?path="+url.QueryEscape(ts.tb), nil, 200, &listing)
	if !listing.Toolbox || len(listing.Entries) != 1 || listing.Entries[0].Name != "tools" {
		t.Fatalf("fs: %+v", listing)
	}
	ts.do(t, "GET", "/api/fs?path=relative", nil, 400, nil)
}

func TestAddToolboxNeedsCreate(t *testing.T) {
	ts := newTestServer(t)
	dir := t.TempDir()
	var res struct {
		CanCreate bool `json:"can_create"`
	}
	ts.do(t, "POST", "/api/toolboxes", map[string]any{"path": dir}, 409, &res)
	if !res.CanCreate {
		t.Fatal("should offer to create")
	}
	ts.do(t, "POST", "/api/toolboxes", map[string]any{"path": dir, "name": "fresh", "create": true}, 200, nil)
	ts.do(t, "POST", "/api/toolboxes/fresh/default", nil, 200, nil)
	ts.do(t, "DELETE", "/api/toolboxes/fresh", nil, 200, nil)
}

// writeShellTool writes a minimal shell tool into dir/<id>.
func writeShellTool(t *testing.T, parent, id, version string) string {
	t.Helper()
	dir := filepath.Join(parent, id)
	os.MkdirAll(dir, 0o755)
	m := `{"schema":1,"id":"` + id + `","version":"` + version + `","name":"Draft ` + id + `","description":"d","entry":{"path":"run"},
	  "inputs":[{"name":"name","type":"string","arg":{"positional":true}}]}`
	os.WriteFile(filepath.Join(dir, "tool.json"), []byte(m), 0o644)
	os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\necho \"draft $1\"\n"), 0o755)
	return dir
}

func TestDrafts(t *testing.T) {
	ts := newTestServer(t)
	draftsDir := filepath.Join(os.Getenv("MAGGOT_DATA_DIR"), "drafts")
	inDrafts := writeShellTool(t, draftsDir, "fresh", "0.1.0")
	elsewhere := writeShellTool(t, t.TempDir(), "outside", "0.1.0")

	// Trying a folder: it shows up as try/<id>, the drafts folder as draft/<id>.
	var added struct{ Ref string }
	ts.do(t, "POST", "/api/drafts", map[string]any{"path": elsewhere}, 200, &added)
	if added.Ref != "try/outside" {
		t.Fatalf("ref %q", added.Ref)
	}
	ts.do(t, "POST", "/api/drafts", map[string]any{"path": inDrafts}, 200, &added)
	if added.Ref != "draft/fresh" {
		t.Fatalf("drafts-folder ref %q", added.Ref)
	}
	ts.do(t, "POST", "/api/drafts", map[string]any{"path": t.TempDir()}, 400, nil)

	var state struct {
		Drafts []draftInfo
		Tools  []toolSummary
	}
	ts.do(t, "GET", "/api/state", nil, 200, &state)
	if len(state.Drafts) != 2 || len(state.Tools) != 1 {
		t.Fatalf("drafts must not be mixed with tools: %+v", state)
	}

	// A draft works like a tool: info, preview, run.
	var tool struct {
		Draft map[string]string
	}
	ts.do(t, "GET", "/api/tool?ref=try/outside", nil, 200, &tool)
	if tool.Draft["kind"] != "try" || tool.Draft["path"] != elsewhere {
		t.Fatalf("draft info: %+v", tool)
	}
	var meta RunMeta
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "draft/fresh", "values": map[string]any{"name": "x"}}, 200, &meta)
	r, _ := ts.s.runs.Active(meta.ID)
	if r != nil {
		<-r.Done()
	}
	if got, _ := ts.s.runs.Get(meta.ID); got.Status != StatusSucceeded {
		t.Fatalf("draft run: %+v", got)
	}

	// Edits show up at once (the manifest is re-read).
	os.WriteFile(filepath.Join(elsewhere, "tool.json"), []byte(`{"schema":1,"id":"outside","version":"0.2.0","name":"Renamed","description":"d","entry":{"path":"run"}}`), 0o644)
	var again struct {
		Manifest struct{ Name, Version string }
	}
	ts.do(t, "GET", "/api/tool?ref=try/outside", nil, 200, &again)
	if again.Manifest.Name != "Renamed" {
		t.Fatalf("not re-read: %+v", again)
	}

	// Drafts cannot be deleted through the tool endpoint.
	ts.do(t, "DELETE", "/api/tool?ref=draft/fresh", nil, 400, nil)
	// Removing a tried folder only forgets it; the files stay.
	ts.do(t, "DELETE", "/api/drafts?ref=try/outside", nil, 200, nil)
	if _, err := os.Stat(elsewhere); err != nil {
		t.Fatal("tried folder must not be deleted")
	}
	// Removing a draft in the drafts folder deletes it.
	ts.do(t, "DELETE", "/api/drafts?ref=draft/fresh", nil, 200, nil)
	if _, err := os.Stat(inDrafts); err == nil {
		t.Fatal("draft folder should be deleted")
	}
	ts.do(t, "GET", "/api/state", nil, 200, &state)
	if len(state.Drafts) != 0 {
		t.Fatalf("drafts left: %+v", state.Drafts)
	}
}

func TestTakeOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	var meta RunMeta
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "worker", "values": map[string]any{"name": "pic"}}, 200, &meta)
	if r, _ := ts.s.runs.Active(meta.ID); r != nil {
		<-r.Done()
	}
	dest := t.TempDir()
	os.WriteFile(filepath.Join(dest, "pic.txt"), []byte("old"), 0o644)
	var conflict struct{ Conflicts []string }
	ts.do(t, "POST", "/api/runs/"+meta.ID+"/take", map[string]any{"dest": dest}, 409, &conflict)
	if len(conflict.Conflicts) != 1 || conflict.Conflicts[0] != "pic.txt" {
		t.Fatalf("conflicts: %+v", conflict)
	}
	var res struct{ Moved []string }
	ts.do(t, "POST", "/api/runs/"+meta.ID+"/take", map[string]any{"dest": dest, "overwrite": true}, 200, &res)
	if len(res.Moved) != 2 {
		t.Fatalf("moved: %+v", res)
	}
	ts.do(t, "GET", "/api/runs/"+meta.ID, nil, 404, nil)
	ts.do(t, "POST", "/api/runs/"+meta.ID+"/take", map[string]any{"dest": ""}, 400, nil)
}

func TestToolFilesViewer(t *testing.T) {
	ts := newTestServer(t)
	dir := filepath.Join(ts.tb, "tools", "worker")
	os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "x", "a.js"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Worker"), 0o644)
	os.WriteFile(filepath.Join(dir, "logo.png"), []byte{0x89, 'P', 'N', 'G', 0, 0, 1}, 0o644)

	var list struct {
		Files []toolFile
	}
	ts.do(t, "GET", "/api/tool/files?ref=worker", nil, 200, &list)
	var paths []string
	for _, f := range list.Files {
		paths = append(paths, f.Path)
	}
	got := strings.Join(paths, ",")
	if !strings.HasPrefix(got, "tool.json,run,README.md") || !strings.Contains(got, "node_modules/") || strings.Contains(got, "a.js") {
		t.Fatalf("files: %s", got)
	}
	var file struct {
		Text   string
		Binary bool
	}
	ts.do(t, "GET", "/api/tool/file?ref=worker&path=run", nil, 200, &file)
	if !strings.HasPrefix(file.Text, "#!/bin/sh") {
		t.Fatalf("text: %+v", file)
	}
	ts.do(t, "GET", "/api/tool/file?ref=worker&path=logo.png", nil, 200, &file)
	if !file.Binary {
		t.Fatal("png should be binary")
	}
	ts.do(t, "GET", "/api/tool/file?ref=worker&path=../../maggot.json", nil, 404, nil)
	ts.do(t, "POST", "/api/tool/open", map[string]any{"ref": "worker"}, 200, nil)
	if len(ts.opened) == 0 || ts.opened[len(ts.opened)-1] != dir {
		t.Fatalf("open folder: %v", ts.opened)
	}
}

func waitWarm(t *testing.T, id string) *warm.Status {
	t.Helper()
	for i := 0; i < 100; i++ {
		if st, _ := warm.Get(id); st != nil && st.Running {
			return st
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not become warm", id)
	return nil
}

func TestWarmOverHTTP(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not installed")
	}
	ts := newTestServer(t)
	if _, err := scaffold.Create(filepath.Join(ts.tb, "tools"), scaffold.Options{ID: "warmui", Lang: "python", Warm: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { warm.Stop("warmui") })
	var info struct {
		Supported bool
		Status    *warm.Status
	}
	ts.do(t, "GET", "/api/tool/warm?ref=warmui", nil, 200, &info)
	if !info.Supported || info.Status != nil {
		t.Fatalf("cold: %+v", info)
	}
	ts.do(t, "POST", "/api/tool/warm", map[string]any{"ref": "warmui", "minutes": 0.5}, 400, nil)
	// Starting returns at once; the tool loads in the background.
	ts.do(t, "POST", "/api/tool/warm", map[string]any{"ref": "warmui", "minutes": 3}, 200, &info)
	if info.Status == nil {
		t.Fatalf("should be loading or warm: %+v", info)
	}
	st := waitWarm(t, "warmui")
	if st.Idle != 3*time.Minute {
		t.Fatalf("idle %v", st.Idle)
	}
	info.Status = nil // the response has no "status" when the tool is cold
	ts.do(t, "DELETE", "/api/tool/warm?ref=warmui", nil, 200, &info)
	if info.Status != nil {
		t.Fatalf("stopped: %+v", info)
	}
	ts.do(t, "POST", "/api/tool/warm", map[string]any{"ref": "worker"}, 400, nil)
}

// A run with keep_warm loads the tool once, waits for it and uses it; the
// next run goes straight to the warm process.
func TestRunKeepsToolWarm(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not installed")
	}
	ts := newTestServer(t)
	if _, err := scaffold.Create(filepath.Join(ts.tb, "tools"), scaffold.Options{ID: "keepme", Lang: "python", Warm: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { warm.Stop("keepme") })
	in := filepath.Join(t.TempDir(), "n.txt")
	os.WriteFile(in, []byte("x"), 0o644)
	runOnce := func() RunMeta {
		var meta RunMeta
		ts.do(t, "POST", "/api/runs", map[string]any{"ref": "keepme", "keep_warm": 7,
			"values": map[string]any{"files": []string{in}, "upper": true}}, 200, &meta)
		r, _ := ts.s.runs.Active(meta.ID)
		if r == nil {
			t.Fatal("run not active")
		}
		<-r.Done()
		got := r.Meta()
		if got.Status != StatusSucceeded {
			log, _ := ts.s.runs.Log(got.ID)
			t.Fatalf("run failed: %+v %+v", got, log)
		}
		return got
	}
	first := runOnce()
	log, _ := ts.s.runs.Log(first.ID)
	var text []string
	for _, m := range log {
		text = append(text, m.Text)
	}
	if !strings.Contains(strings.Join(text, "\n"), "Loading into memory") {
		t.Fatalf("first run should say it loads: %v", text)
	}
	st := waitWarm(t, "keepme")
	if st.Jobs != 1 || st.Idle != 7*time.Minute {
		t.Fatalf("first run should go to the warm process: %+v", st)
	}
	runOnce()
	if st, _ := warm.Get("keepme"); st == nil || st.Jobs != 2 {
		t.Fatalf("second run: %+v", st)
	}
	// Without keep_warm a cold tool stays cold.
	warm.Stop("keepme")
	var meta RunMeta
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "keepme", "values": map[string]any{"files": []string{in}}}, 200, &meta)
	if r, _ := ts.s.runs.Active(meta.ID); r != nil {
		<-r.Done()
	}
	if st, _ := warm.Get("keepme"); st != nil {
		t.Fatalf("should stay cold: %+v", st)
	}
}

func TestPresetsOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	var res struct {
		Use      string
		Warnings []string
		Presets  []presets.Preset
	}
	ts.do(t, "POST", "/api/tool/presets", map[string]any{"ref": "worker", "label": "Ann (loud)", "scope": "toolbox",
		"values": map[string]any{"name": "ann", "bogus": 1}}, 200, &res)
	if res.Use != "toolbox/ann-loud" || len(res.Presets) != 1 || res.Presets[0].Values["bogus"] != nil {
		t.Fatalf("save: %+v", res)
	}
	ts.do(t, "POST", "/api/tool/presets", map[string]any{"ref": "worker", "label": "  ", "scope": "personal"}, 400, nil)
	ts.do(t, "POST", "/api/tool/presets", map[string]any{"ref": "worker", "label": "x", "scope": "elsewhere"}, 400, nil)
	ts.do(t, "GET", "/api/tool/presets?ref=worker", nil, 200, &res)
	if len(res.Presets) != 1 || res.Presets[0].Label != "Ann (loud)" {
		t.Fatalf("list: %+v", res)
	}
	ts.do(t, "DELETE", "/api/tool/presets?ref=worker&preset=toolbox/ann-loud", nil, 200, &res)
	if len(res.Presets) != 0 {
		t.Fatalf("delete: %+v", res)
	}
}

// Secrets are stored by tool id, so a secret saved while trying a draft is
// there for the installed tool after import.
func TestSecretsCarryOverFromDraft(t *testing.T) {
	ts := newTestServer(t)
	draftsDir := filepath.Join(os.Getenv("MAGGOT_DATA_DIR"), "drafts")
	dir := filepath.Join(draftsDir, "api-tool")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "tool.json"), []byte(`{"schema":1,"id":"api-tool","version":"0.1.0","name":"API","description":"d",
	  "entry":{"path":"run"},"env":[{"name":"API_KEY","secret":true,"required":true}]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\necho \"key=$API_KEY\"\n"), 0o755)

	ts.do(t, "POST", "/api/tool/secret", map[string]any{"ref": "draft/api-tool", "name": "API_KEY", "value": "s3cret"}, 200, nil)

	var staged struct{ Token string }
	ts.do(t, "POST", "/api/import", map[string]any{"path": dir}, 200, &staged)
	ts.do(t, "POST", "/api/import/"+staged.Token, map[string]any{}, 200, nil)

	var tool struct {
		Env []struct {
			Name, Source string
			Set          bool
		}
	}
	ts.do(t, "GET", "/api/tool?ref=t/api-tool", nil, 200, &tool)
	if len(tool.Env) != 1 || !tool.Env[0].Set || tool.Env[0].Source != "store" {
		t.Fatalf("installed tool should see the secret: %+v", tool)
	}
	// And it reaches the program when run.
	var meta RunMeta
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "t/api-tool"}, 200, &meta)
	if r, _ := ts.s.runs.Active(meta.ID); r != nil {
		<-r.Done()
	}
	if out := ts.s.runs.stdoutText(meta); out != "key=s3cret\n" {
		t.Fatalf("run output %q", out)
	}
	// The secret never lands in the tool folder.
	installed := filepath.Join(ts.tb, "tools", "api-tool")
	filepath.WalkDir(installed, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), "s3cret") {
				t.Errorf("secret found in %s", p)
			}
		}
		return nil
	})
}

func TestFSMedia(t *testing.T) {
	ts := newTestServer(t)
	dir := t.TempDir()
	png := filepath.Join(dir, "a.png")
	os.WriteFile(png, []byte("\x89PNG\r\n\x1a\nfake"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("secret"), 0o644)
	get := func(p string) *http.Response {
		r, err := ts.client.Get(ts.URL + "/api/fs/media?path=" + url.QueryEscape(p))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r
	}
	if r := get(png); r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("png: %d %v", r.StatusCode, r.Header)
	}
	if r := get(filepath.Join(dir, "notes.txt")); r.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("txt served: %d", r.StatusCode)
	}
	if r := get(filepath.Join(dir, "gone.png")); r.StatusCode != 404 {
		t.Fatalf("missing: %d", r.StatusCode)
	}
	if r := get("rel/a.png"); r.StatusCode != 400 {
		t.Fatalf("relative: %d", r.StatusCode)
	}
	os.Mkdir(filepath.Join(dir, "d.png"), 0o755)
	if r := get(filepath.Join(dir, "d.png")); r.StatusCode != 404 {
		t.Fatalf("dir: %d", r.StatusCode)
	}
	// Without the cookie: refused.
	r, _ := http.Get(ts.URL + "/api/fs/media?path=" + url.QueryEscape(png))
	if r.StatusCode != 401 {
		t.Fatalf("no auth: %d", r.StatusCode)
	}
}

func TestArchiveAndRestore(t *testing.T) {
	ts := newTestServer(t)
	ts.do(t, "POST", "/api/archive?ref=worker", nil, 200, nil)
	var st struct {
		Tools    []struct{ Ref string } `json:"tools"`
		Archived []struct {
			Kind, Name, Toolbox string
		} `json:"archived"`
	}
	ts.do(t, "GET", "/api/state", nil, 200, &st)
	for _, tl := range st.Tools {
		if strings.HasSuffix(tl.Ref, "/worker") {
			t.Fatal("archived tool still listed")
		}
	}
	if len(st.Archived) != 1 || st.Archived[0].Name != "worker" || st.Archived[0].Kind != "tool" {
		t.Fatalf("archived: %+v", st.Archived)
	}
	ts.do(t, "POST", "/api/restore", map[string]string{"toolbox": st.Archived[0].Toolbox, "kind": "tool", "name": "../worker"}, 404, nil)
	ts.do(t, "POST", "/api/restore", map[string]string{"toolbox": st.Archived[0].Toolbox, "kind": "tool", "name": "worker"}, 200, nil)
	ts.do(t, "GET", "/api/tool?ref=worker", nil, 200, nil)
}

func TestArchivedItemFilesAreReadable(t *testing.T) {
	ts := newTestServer(t)
	ts.do(t, "POST", "/api/archive?ref=worker", nil, 200, nil)
	var st struct {
		Archived []struct{ Toolbox, Name string }
	}
	ts.do(t, "GET", "/api/state", nil, 200, &st)
	ref := st.Archived[0].Toolbox + "/" + st.Archived[0].Name
	var files struct{ Files []struct{ Path string } }
	ts.do(t, "GET", "/api/tool/files?archived=1&ref="+ref, nil, 200, &files)
	if len(files.Files) == 0 || files.Files[0].Path != "tool.json" {
		t.Fatalf("files: %+v", files)
	}
	var file struct{ Text string }
	ts.do(t, "GET", "/api/tool/file?archived=1&path=tool.json&ref="+ref, nil, 200, &file)
	if !strings.Contains(file.Text, `"worker"`) {
		t.Fatalf("file: %q", file.Text)
	}
	ts.do(t, "GET", "/api/tool/files?archived=1&ref="+st.Archived[0].Toolbox+"/..", nil, 404, nil)
	ts.do(t, "GET", "/api/tool/file?archived=1&path=../../maggot.json&ref="+ref, nil, 404, nil)
	ts.do(t, "GET", "/api/tool/files?archived=1&ref="+st.Archived[0].Toolbox+"/nope", nil, 404, nil)
}
