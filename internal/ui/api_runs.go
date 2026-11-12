package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/warm"
	"github.com/nxnom/maggot/internal/workspace"
)

func (s *Server) apiStartRun(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Ref        string         `json:"ref"`
		Values     map[string]any `json:"values"`
		SkipChecks bool           `json:"skip_checks"`
		OutDir     string         `json:"out_dir"`
		// KeepWarm (minutes) keeps the tool in memory after this run.
		KeepWarm float64 `json:"keep_warm"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	_, t, err := s.resolve(r, req.Ref)
	if err != nil {
		return err
	}
	if !req.SkipChecks {
		if missing := workspace.Missing(t, s.store, s.checker.LookPath); len(missing) > 0 {
			return &httpError{code: http.StatusConflict, msg: "missing " + strings.Join(missing, ", "),
				extra: map[string]any{"missing": missing}}
		}
	}
	env, err := workspace.SecretEnv(s.store, t)
	if err != nil {
		return err
	}
	opts := StartOptions{Values: req.Values, OutDir: req.OutDir, Origin: "ui"}
	if t.Manifest.Warm != nil {
		id := t.Manifest.ID
		st, _ := warm.Get(id)
		if st == nil && req.KeepWarm > 0 {
			idle := time.Duration(req.KeepWarm * float64(time.Minute))
			if _, _, err := warm.StartAsync(t, idle, env); err != nil {
				return err
			}
			st, _ = warm.Get(id)
		}
		// While it loads, this run waits and then uses it, instead of
		// loading a second copy.
		if st != nil && !st.Running {
			opts.Before = func(log func(string), stop <-chan struct{}) {
				log("Loading into memory. Later runs skip this while the tool stays warm.")
				if !warm.WaitLoaded(id, 15*time.Minute, stop, log) {
					log("The warm process is not ready, so this run loads the tool itself.")
				}
			}
		}
	}
	env = append(env, warm.Env(t)...)
	opts.ExtraEnv = env
	if req.OutDir != "" && !filepath.IsAbs(config.ExpandHome(req.OutDir)) {
		return errors.New("choose an output folder (absolute path)")
	}
	run, err := s.runs.StartWith(t, opts)
	if err != nil {
		return err
	}
	return ok(w, run.Meta())
}

func (s *Server) apiRuns(w http.ResponseWriter, r *http.Request) error {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	list := s.runs.List(r.URL.Query().Get("tool"), limit)
	if list == nil {
		list = []RunMeta{}
	}
	return ok(w, map[string]any{"runs": list})
}

func (s *Server) apiRun(w http.ResponseWriter, r *http.Request) error {
	meta, err := s.runs.Get(r.PathValue("id"))
	if err != nil {
		return err
	}
	res := map[string]any{"run": meta, "outputs": s.runs.Outputs(meta)}
	if meta.OutputKind == "stdout" && meta.Status != StatusRunning {
		text := s.runs.stdoutText(meta)
		if len(text) > 512<<10 {
			text = text[:512<<10]
		}
		res["stdout"] = text
	}
	return ok(w, res)
}

// apiDeleteRuns deletes all finished runs of one tool (?tool=<id>).
func (s *Server) apiDeleteRuns(w http.ResponseWriter, r *http.Request) error {
	tool := r.URL.Query().Get("tool")
	if tool == "" {
		return errors.New("name a tool")
	}
	return ok(w, map[string]int{"deleted": s.runs.DeleteAll(tool)})
}

// apiTakeRun moves a run's output files into a folder and deletes the run.
func (s *Server) apiTakeRun(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Dest      string   `json:"dest"`
		Files     []string `json:"files"`
		Overwrite bool     `json:"overwrite"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Dest) == "" {
		return errors.New("choose a destination folder")
	}
	moved, err := s.runs.Take(r.PathValue("id"), req.Dest, req.Files, req.Overwrite)
	var ce *ConflictError
	if errors.As(err, &ce) {
		return &httpError{code: http.StatusConflict, msg: err.Error(), extra: map[string]any{"conflicts": ce.Files}}
	}
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"moved": moved})
}

func (s *Server) apiCancelRun(w http.ResponseWriter, r *http.Request) error {
	if err := s.runs.Cancel(r.PathValue("id")); err != nil {
		return err
	}
	return ok(w, map[string]bool{"ok": true})
}

func (s *Server) apiDeleteRun(w http.ResponseWriter, r *http.Request) error {
	if err := s.runs.Delete(r.PathValue("id")); err != nil {
		return err
	}
	return ok(w, map[string]bool{"ok": true})
}

// apiStream sends a run's messages as server-sent events: everything so far,
// then live messages until the run ends.
func (s *Server) apiStream(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		return errors.New("streaming not supported")
	}
	send := func(m Message) {
		b, _ := json.Marshal(m)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	start := func() {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
	}

	run, active := s.runs.Active(id)
	if !active {
		meta, err := s.runs.Get(id)
		if err != nil {
			return err
		}
		start()
		if meta.Status == StatusRunning {
			// Run by another maggot process (e.g. `maggot run --review`):
			// follow its log file until it ends.
			return s.followFile(r, id, send, flusher)
		}
		log, _ := s.runs.Log(id)
		for _, m := range log {
			send(m)
		}
		send(Message{Seq: len(log) + 1, Type: "status", Status: &meta})
		flusher.Flush()
		return nil
	}
	hist, ch, stop := run.Subscribe()
	defer stop()
	start()
	for _, m := range hist {
		send(m)
	}
	flusher.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case m := <-ch:
			send(m)
			// Batch whatever else is queued before flushing.
			for more := true; more; {
				select {
				case m2 := <-ch:
					send(m2)
				default:
					more = false
				}
			}
			flusher.Flush()
		case <-run.Done():
			for len(ch) > 0 {
				send(<-ch)
			}
			meta := run.Meta()
			send(Message{Type: "status", Status: &meta})
			flusher.Flush()
			return nil
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return nil
		}
	}
}

// followFile streams a run that another process writes, by polling its log.
func (s *Server) followFile(r *http.Request, id string, send func(Message), flusher http.Flusher) error {
	sent := 0
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		meta, err := s.runs.Get(id)
		if err != nil {
			return nil
		}
		log, _ := s.runs.Log(id)
		for _, m := range log[min(sent, len(log)):] {
			send(m)
		}
		sent = max(sent, len(log))
		if meta.Status != StatusRunning {
			send(Message{Type: "status", Status: &meta})
			flusher.Flush()
			return nil
		}
		flusher.Flush()
		select {
		case <-tick.C:
		case <-r.Context().Done():
			return nil
		}
	}
}

// insideDir resolves rel inside dir and refuses anything that escapes it,
// including through symlinks.
func insideDir(dir, rel string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	realP, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if realP != realDir && !strings.HasPrefix(realP, realDir+string(filepath.Separator)) {
		return "", os.ErrNotExist
	}
	return realP, nil
}

func (s *Server) apiRunFile(w http.ResponseWriter, r *http.Request) error {
	meta, err := s.runs.Get(r.PathValue("id"))
	if err != nil {
		return err
	}
	p, err := insideDir(meta.OutDir, r.PathValue("path"))
	if err != nil {
		return err
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return os.ErrNotExist
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(p)))
	}
	mt := mimeOf(p)
	w.Header().Set("Content-Type", mt)
	// Output files are untrusted: a sandbox keeps HTML or SVG from running
	// scripts. PDFs are left out because the sandbox also blocks the
	// browser's PDF viewer.
	if mt != "application/pdf" {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	http.ServeContent(w, r, filepath.Base(p), info.ModTime(), f)
	return nil
}

// apiReveal shows a run's output folder (or one file in it) in the file manager.
func (s *Server) apiReveal(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Run  string `json:"run"`
		Path string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	meta, err := s.runs.Get(req.Run)
	if err != nil {
		return err
	}
	target := meta.OutDir
	reveal := false
	if req.Path != "" {
		if target, err = insideDir(meta.OutDir, req.Path); err != nil {
			return err
		}
		reveal = true
	}
	if err := s.open(target, reveal); err != nil {
		return err
	}
	return ok(w, map[string]bool{"ok": true})
}
