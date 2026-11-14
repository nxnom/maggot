// Package ui serves maggot's local web UI: a form for every tool, generated
// from tool.json, with live runs and their output files.
package ui

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/nxnom/maggot/internal/secrets"
	"github.com/nxnom/maggot/internal/workspace"
)

//go:embed web
var webFS embed.FS

const cookieName = "maggot_token"

// Server is the web UI. Create it with New.
type Server struct {
	Version string
	// Selector limits the UI to one toolbox (same as --toolbox).
	Selector string
	Token    string
	// Worker is the command that runs a UI run in the background, so runs
	// keep going when the UI stops (`maggot ui-run`; the spec path is
	// added). Nil runs tools inside the UI process.
	Worker []string

	runs    *Manager
	data    string
	store   secrets.Store
	checker *doctor.Checker
	// open is how files/folders are revealed (replaced in tests).
	open func(path string, reveal bool) error

	mu     sync.Mutex
	hosts  map[string]bool
	staged map[string]*stagedImport
	tried  []string // folders being tried; memory only
}

type stagedImport struct {
	staged  *pack.Staged
	toolbox string
	created time.Time
	src     string // source folder, when imported from a folder (not an archive)
}

// New prepares a server using the user's data dir and secret store.
func New(version, selector string) (*Server, error) {
	data, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	store, err := secrets.Default()
	if err != nil {
		return nil, err
	}
	token, err := loadToken(filepath.Join(data, "ui-token"))
	if err != nil {
		return nil, err
	}
	s := &Server{
		Version: version, Selector: selector, Token: token,
		runs: NewManager(filepath.Join(data, "runs"), cfg.Keep()), data: data,
		store: store, checker: doctor.New(), open: openPath,
		hosts: map[string]bool{}, staged: map[string]*stagedImport{},
	}
	s.cleanUploads(7 * 24 * time.Hour)
	return s, nil
}

// loadToken keeps one access token per user, so a bookmarked
// http://127.0.0.1:<port>/ keeps working after the first visit.
func loadToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	t := randomHex(24)
	return t, config.WriteFileAtomic(path, []byte(t+"\n"), 0o600)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Listen binds to 127.0.0.1 (port 0 picks a free port) and returns the
// listener and the URL to open, which carries the access token.
func (s *Server) Listen(port int) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil && port != 0 {
		ln, err = net.Listen("tcp", "127.0.0.1:0") // preferred port busy
	}
	if err != nil {
		return nil, "", err
	}
	p := ln.Addr().(*net.TCPAddr).Port
	s.AllowHost(fmt.Sprintf("127.0.0.1:%d", p))
	s.AllowHost(fmt.Sprintf("localhost:%d", p))
	return ln, fmt.Sprintf("http://127.0.0.1:%d/?token=%s", p, s.Token), nil
}

// AllowHost adds a Host header value the server accepts.
func (s *Server) AllowHost(h string) {
	s.mu.Lock()
	s.hosts[h] = true
	s.mu.Unlock()
}

func (s *Server) hostOK(h string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hosts[h]
}

// Handler returns the HTTP handler with all routes and checks.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(webFS, "web")
	static := http.FileServerFS(web)
	mux.HandleFunc("GET /{$}", s.index)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", static))

	api := func(pattern string, h func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, s.guard(h))
	}
	api("GET /api/state", s.apiState)
	api("POST /api/toolboxes", s.apiAddToolbox)
	api("DELETE /api/toolboxes/{name}", s.apiRemoveToolbox)
	api("POST /api/toolboxes/{name}/default", s.apiDefaultToolbox)
	api("GET /api/fs", s.apiFS)
	api("GET /api/fs/find", s.apiFSFind)
	api("GET /api/fs/media", s.apiFSMedia)
	api("GET /api/search", s.apiSearch)
	api("GET /api/tool", s.apiTool)
	api("DELETE /api/tool", s.apiRemoveTool)
	api("POST /api/archive", s.apiArchive)
	api("POST /api/restore", s.apiRestore)
	api("GET /api/tool/health", s.apiHealth)
	api("POST /api/tool/install", s.apiInstall)
	api("POST /api/tool/secret", s.apiSetSecret)
	api("DELETE /api/tool/secret", s.apiDeleteSecret)
	api("POST /api/preview", s.apiPreview)
	api("GET /api/export", s.apiExport)
	api("POST /api/import", s.apiImportStage)
	api("POST /api/import/{token}", s.apiImportCommit)
	api("DELETE /api/import/{token}", s.apiImportDiscard)
	api("POST /api/uploads", s.apiUpload)
	api("GET /api/runs", s.apiRuns)
	api("POST /api/runs", s.apiStartRun)
	api("DELETE /api/runs", s.apiDeleteRuns)
	api("GET /api/runs/{id}", s.apiRun)
	api("DELETE /api/runs/{id}", s.apiDeleteRun)
	api("POST /api/runs/{id}/cancel", s.apiCancelRun)
	api("POST /api/runs/{id}/take", s.apiTakeRun)
	api("GET /api/runs/{id}/stream", s.apiStream)
	api("GET /api/runs/{id}/log", s.apiRunLog)
	api("GET /api/runs/{id}/log.txt", s.apiRunLogText)
	api("GET /api/runs/{id}/files/{path...}", s.apiRunFile)
	api("POST /api/reveal", s.apiReveal)
	api("POST /api/drafts", s.apiAddDraft)
	api("GET /api/tool/files", s.apiToolFiles)
	api("GET /api/tool/file", s.apiToolFile)
	api("POST /api/tool/open", s.apiOpenTool)
	api("GET /api/tool/presets", s.apiPresets)
	api("POST /api/tool/presets", s.apiSavePreset)
	api("DELETE /api/tool/presets", s.apiDeletePreset)
	api("GET /api/tool/warm", s.apiWarmStatus)
	api("POST /api/tool/warm", s.apiWarmStart)
	api("DELETE /api/tool/warm", s.apiWarmStop)
	api("DELETE /api/drafts", s.apiRemoveDraft)
	api("GET /api/tool/changes", s.apiChanges)
	api("GET /api/tool/diff", s.apiDiff)
	api("GET /api/skill", s.apiSkill)
	api("POST /api/skill/agents", s.apiSkillAgents)
	api("POST /api/skill/secret", s.apiSetSkillSecret)
	api("DELETE /api/skill/secret", s.apiDeleteSkillSecret)

	return s.checkHost(mux)
}

// checkHost blocks DNS rebinding: only our own host names are served.
func (s *Server) checkHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r.Host) {
			http.Error(w, "unknown host", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authed(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.Token)) == 1
}

// index swaps the one-time URL token for a cookie, then serves the app.
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if t := r.URL.Query().Get("token"); t != "" {
		if subtle.ConstantTimeCompare([]byte(t), []byte(s.Token)) != 1 {
			http.Error(w, "wrong token; open the link printed by `maggot ui`", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.Token, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.authed(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>maggot</title><p style="font:16px system-ui;margin:3rem">Open the link that <code>maggot ui</code> printed in your terminal.</p>`)
		return
	}
	b, _ := webFS.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; frame-src 'self'; style-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'none'")
	w.Write(b)
}

// httpError carries a status code and extra JSON fields.
type httpError struct {
	code  int
	msg   string
	extra map[string]any
}

func (e *httpError) Error() string { return e.msg }

func errStatus(code int, format string, args ...any) error {
	return &httpError{code: code, msg: fmt.Sprintf(format, args...)}
}

// guard checks the token (and Origin for writes), runs h and writes errors as JSON.
func (s *Server) guard(h func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in; open the link printed by `maggot ui`"})
			return
		}
		if r.Method != http.MethodGet {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || !s.hostOK(u.Host) {
					writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request"})
					return
				}
			}
		}
		if err := h(w, r); err != nil {
			code := http.StatusBadRequest
			body := map[string]any{"error": err.Error()}
			var he *httpError
			switch {
			case errors.As(err, &he):
				code = he.code
				for k, v := range he.extra {
					body[k] = v
				}
			case errors.Is(err, fs.ErrNotExist):
				code = http.StatusNotFound
			}
			writeJSON(w, code, body)
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func ok(w http.ResponseWriter, v any) error {
	writeJSON(w, http.StatusOK, v)
	return nil
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func (s *Server) workspace() (*workspace.Workspace, error) {
	return workspace.Load(s.Selector, "")
}

// openPath reveals a file in the OS file manager, or opens a folder/URL.
func openPath(path string, reveal bool) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if reveal {
			cmd = exec.Command("open", "-R", path)
		} else {
			cmd = exec.Command("open", path)
		}
	case "windows":
		if reveal {
			cmd = exec.Command("explorer", "/select,"+path)
		} else {
			cmd = exec.Command("cmd", "/c", "start", "", path)
		}
	default:
		if reveal {
			path = filepath.Dir(path)
		}
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// OpenBrowser opens a URL in the default browser.
func OpenBrowser(u string) error { return openPath(u, false) }

func (s *Server) cleanUploads(maxAge time.Duration) {
	root := filepath.Join(s.data, "uploads")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// Close stops active runs and discards staged imports.
func (s *Server) Close() {
	s.runs.StopAll(5 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, st := range s.staged {
		st.staged.Discard()
		delete(s.staged, k)
	}
}

func strip(s string) string { return strings.TrimSpace(s) }
