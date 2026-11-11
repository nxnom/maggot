package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/nxnom/maggot/internal/secrets"
	"github.com/nxnom/maggot/internal/toolbox"
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
	if r2.StatusCode != 200 || string(body) != "hello ann\n" || r2.Header.Get("Content-Security-Policy") != "sandbox" {
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
	ts.do(t, "DELETE", "/api/runs/"+meta.ID, nil, 200, nil)
	ts.do(t, "GET", "/api/runs/"+meta.ID, nil, 404, nil)
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
