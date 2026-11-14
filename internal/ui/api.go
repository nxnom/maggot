package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/nxnom/maggot/internal/runner"
	"github.com/nxnom/maggot/internal/search"
	"github.com/nxnom/maggot/internal/skill"
	"github.com/nxnom/maggot/internal/testlog"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/warm"
	"github.com/nxnom/maggot/internal/workspace"
)

type toolSummary struct {
	Ref         string   `json:"ref"`
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Categories  []string `json:"categories"`
	Toolbox     string   `json:"toolbox"`
	// For the home page cards.
	Tested  string   `json:"tested"`            // passed, failed, old, never
	Missing []string `json:"missing,omitempty"` // binaries/env needed before it can run
	Warm    bool     `json:"warm,omitempty"`    // can stay in memory
	// Draft is a draft with the same id: a new version being worked on.
	Draft *toolLink `json:"draft,omitempty"`
}

func summary(t *toolbox.Tool) toolSummary {
	c := t.Manifest.Categories
	if c == nil {
		c = []string{}
	}
	return toolSummary{Ref: t.Ref(), ID: t.Manifest.ID, Version: t.Manifest.Version, Name: t.Manifest.Name,
		Description: t.Manifest.Description, Categories: c, Toolbox: t.Toolbox.Handle, Warm: t.Manifest.Warm != nil}
}

// testedLabel says how the last test went for this version of the tool.
func testedLabel(rec *testlog.Record, version string) string {
	switch {
	case rec == nil:
		return "never"
	case rec.Version != version:
		return "old"
	case rec.OK:
		return "passed"
	}
	return "failed"
}

func (s *Server) apiState(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.workspace()
	if err != nil {
		return err
	}
	type tbRow struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Default bool   `json:"default"`
		OK      bool   `json:"ok"`
		Error   string `json:"error,omitempty"`
		Tools   int    `json:"tools"`
	}
	tools, _ := ws.Set.Tools()
	counts := map[string]int{}
	rows := []toolSummary{}
	tested, _ := testlog.Load()
	drafts := s.drafts()
	if drafts == nil {
		drafts = []draftInfo{}
	}
	linkDrafts(drafts, tools, ws.Config.Default)
	for _, t := range tools {
		counts[t.Toolbox.Handle]++
		row := summary(t)
		var rec *testlog.Record
		if r, found := tested[t.Ref()]; found {
			rec = &r
		}
		row.Tested = testedLabel(rec, t.Manifest.Version)
		row.Missing = workspace.Missing(t, s.store, s.checker.LookPath)
		if d := draftFor(t.Manifest.ID, drafts); d != nil {
			row.Draft = linkToDraft(*d)
		}
		rows = append(rows, row)
	}
	warmNow := warm.List()
	if warmNow == nil {
		warmNow = []warm.Status{}
	}
	boxes := []tbRow{}
	for _, tb := range ws.Set.Toolboxes {
		boxes = append(boxes, tbRow{Name: tb.Handle, Path: tb.Path, Default: tb.Handle == ws.Config.Default, OK: true, Tools: counts[tb.Handle]})
	}
	for _, p := range ws.Problems {
		boxes = append(boxes, tbRow{Name: p.Toolbox, Path: p.Path, Default: p.Toolbox == ws.Config.Default, Error: p.Error})
	}
	home, _ := os.UserHomeDir()
	draftsDir, _ := config.DraftsDir()
	skills, skillDrafts := s.skillState(ws)
	archived := ws.Set.Archived()
	if archived == nil {
		archived = []toolbox.Archived{}
	}
	return ok(w, map[string]any{
		"version": s.Version, "config_path": ws.Config.Path(), "data_dir": s.data, "home": home,
		"selected": s.Selector != "", "toolboxes": boxes, "tools": rows, "problems": ws.AllProblems(),
		"drafts": drafts, "drafts_dir": draftsDir, "warm": warmNow,
		"skills": skills, "skill_drafts": skillDrafts, "archived": archived,
	})
}

func (s *Server) apiAddToolbox(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Path   string `json:"path"`
		Name   string `json:"name"`
		Create bool   `json:"create"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if s.Selector != "" {
		return errors.New("maggot ui was started with --toolbox; restart it without --toolbox to manage toolboxes")
	}
	path := config.ExpandHome(strip(req.Path))
	if !filepath.IsAbs(path) {
		return errors.New("choose a folder (absolute path)")
	}
	tb, err := toolbox.Open(path, "")
	if err != nil {
		if !req.Create {
			return &httpError{code: http.StatusConflict, msg: "this folder is not a toolbox yet", extra: map[string]any{"can_create": true}}
		}
		name := req.Name
		if name == "" {
			name = workspace.HandleFor(path, "")
		}
		if tb, err = toolbox.Init(path, name); err != nil {
			return err
		}
	}
	name := strip(req.Name)
	if name == "" {
		name = workspace.HandleFor(tb.Path, tb.Meta.Name)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Add(name, tb.Path); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	return ok(w, map[string]string{"name": name, "path": tb.Path})
}

func (s *Server) apiRemoveToolbox(w http.ResponseWriter, r *http.Request) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Remove(r.PathValue("name")); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	return ok(w, map[string]bool{"ok": true})
}

func (s *Server) apiDefaultToolbox(w http.ResponseWriter, r *http.Request) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.SetDefault(r.PathValue("name")); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	return ok(w, map[string]bool{"ok": true})
}

// apiFS lists a folder for the folder/file picker.
type fsEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Rel     string `json:"rel,omitempty"` // search results: path below the searched folder
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Toolbox bool   `json:"toolbox,omitempty"`
}

// fsPath cleans a folder path from the picker; a file path means its folder.
func (s *Server) fsPath(p string) (string, error) {
	p = config.ExpandHome(p)
	if p == "" {
		p, _ = os.UserHomeDir()
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("path must be absolute")
	}
	p = filepath.Clean(p)
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		p = filepath.Dir(p)
	}
	return p, nil
}

// apiFSMedia serves a local image, video or audio file so the form can
// preview the files chosen for a tool. Other kinds of files are refused.
func (s *Server) apiFSMedia(w http.ResponseWriter, r *http.Request) error {
	p := config.ExpandHome(r.URL.Query().Get("path"))
	if !filepath.IsAbs(p) {
		return errors.New("path must be absolute")
	}
	p = filepath.Clean(p)
	mt := mimeOf(p)
	if kind, _, _ := strings.Cut(mt, "/"); kind != "image" && kind != "video" && kind != "audio" {
		return &httpError{code: http.StatusUnsupportedMediaType, msg: "only images, video and audio can be previewed"}
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return os.ErrNotExist
	}
	w.Header().Set("Content-Type", mt)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "sandbox") // svg can hold scripts
	http.ServeContent(w, r, filepath.Base(p), info.ModTime(), f)
	return nil
}

func (s *Server) apiFS(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	p, err := s.fsPath(q.Get("path"))
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return err
	}
	hidden := q.Get("hidden") == "1"
	files := q.Get("files") == "1"
	accept := parseAccept(q.Get("accept"))
	filtered := 0 // files left out because they don't match accept
	out := []fsEntry{}
	for _, e := range entries {
		if !hidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(p, e.Name())
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			if info, err := os.Stat(full); err == nil {
				isDir = info.IsDir()
			}
		}
		if !isDir && !files {
			continue
		}
		if !isDir && !accepts(e.Name(), accept) {
			filtered++
			continue
		}
		en := fsEntry{Name: e.Name(), Path: full, Dir: isDir}
		if !isDir {
			if info, err := e.Info(); err == nil {
				en.Size = info.Size()
			}
		} else if _, err := os.Stat(filepath.Join(full, toolbox.FileName)); err == nil {
			en.Toolbox = true
		}
		out = append(out, en)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	_, isToolbox := os.Stat(filepath.Join(p, toolbox.FileName))
	return ok(w, map[string]any{"path": p, "parent": parent, "entries": out, "toolbox": isToolbox == nil, "filtered": filtered})
}

func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.workspace()
	if err != nil {
		return err
	}
	tools, _ := ws.Set.Tools()
	q := r.URL.Query().Get("q")
	refs := []string{}
	for _, h := range search.Rank(tools, q) {
		refs = append(refs, h.Tool.Ref())
	}
	// Nothing matched: the most similar tools, marked as not a direct match.
	closest := []string{}
	if len(refs) == 0 {
		for _, t := range search.Closest(tools, q, 3) {
			closest = append(closest, t.Ref())
		}
	}
	skills, _ := ws.Set.Skills()
	skillRefs := []string{}
	for _, h := range search.RankSkills(skills, q) {
		skillRefs = append(skillRefs, h.Skill.Ref())
	}
	return ok(w, map[string]any{"refs": refs, "closest": closest, "skills": skillRefs})
}

func (s *Server) resolve(r *http.Request, ref string) (*workspace.Workspace, *toolbox.Tool, error) {
	ws, err := s.workspace()
	if err != nil {
		return nil, nil, err
	}
	if ref == "" {
		ref = r.URL.Query().Get("ref")
	}
	if isDraftRef(ref) {
		d, err := s.draft(ref)
		if err != nil {
			return nil, nil, err
		}
		return ws, d.tool, nil
	}
	t, err := ws.Set.Resolve(ref)
	if err != nil {
		return nil, nil, &httpError{code: http.StatusNotFound, msg: err.Error()}
	}
	return ws, t, nil
}

func (s *Server) apiTool(w http.ResponseWriter, r *http.Request) error {
	ws, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	readme, _ := os.ReadFile(filepath.Join(t.Dir, "README.md"))
	argv, _ := runner.Argv(t.Dir, t.Manifest, nil)
	// A draft links to the installed tool it changes, and back.
	tools, _ := ws.Set.Tools()
	var draft map[string]any
	var draftOf *toolLink
	if isDraftRef(t.Ref()) {
		draft = map[string]any{"kind": t.Toolbox.Handle, "path": t.Dir}
		if it := installedFor(t.Manifest.ID, tools, ws.Config.Default); it != nil {
			draft["of"] = linkTo(it)
		}
	} else if d := draftFor(t.Manifest.ID, s.drafts()); d != nil {
		draftOf = linkToDraft(*d)
	}
	var last *testlog.Record
	if all, _ := testlog.Load(); all != nil && draft == nil {
		if rec, found := all[t.Ref()]; found {
			last = &rec
		}
	}
	return ok(w, map[string]any{
		"ref": t.Ref(), "toolbox": t.Toolbox.Handle, "dir": t.Dir, "manifest": t.Manifest,
		"readme": string(readme), "direct": shellJoin(argv), "env": workspace.Env(s.store, t),
		"last_test": last, "draft": draft, "has_draft": draftOf, "warm": warmInfo(t.Manifest.ID, t.Manifest.Warm != nil),
	})
}

func (s *Server) apiRemoveTool(w http.ResponseWriter, r *http.Request) error {
	if isDraftRef(r.URL.Query().Get("ref")) {
		return errors.New("use the draft's Remove button")
	}
	if isSkillReq(r) {
		_, sk, _, err := s.resolveSkill(r.URL.Query().Get("ref"))
		if err != nil {
			return err
		}
		// Links in agents' skills folders would break; remove them too.
		if home, err := os.UserHomeDir(); err == nil {
			for _, ag := range skill.Agents() {
				skill.Unlink(home, ag, sk.Meta.Name, sk.Dir, false)
			}
		}
		if err := os.RemoveAll(sk.Dir); err != nil {
			return err
		}
		return ok(w, map[string]string{"removed": sk.Ref()})
	}
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	if err := os.RemoveAll(t.Dir); err != nil {
		return err
	}
	return ok(w, map[string]string{"removed": t.Ref()})
}

// apiArchive moves a tool or skill (ref, optional kind=skill) to the archive.
func (s *Server) apiArchive(w http.ResponseWriter, r *http.Request) error {
	ref := r.URL.Query().Get("ref")
	if isDraftRef(ref) {
		return errors.New("drafts cannot be archived; use the draft's Remove button")
	}
	if isSkillReq(r) {
		_, sk, draft, err := s.resolveSkill(ref)
		if err != nil {
			return err
		}
		if draft != nil {
			return errors.New("drafts cannot be archived; use the draft's Remove button")
		}
		// Links in agents' skills folders would break, and agents would still find it.
		if home, err := os.UserHomeDir(); err == nil {
			for _, ag := range skill.Agents() {
				skill.Unlink(home, ag, sk.Meta.Name, sk.Dir, false)
			}
		}
		if err := sk.Toolbox.Archive(toolbox.KindSkill, sk.Meta.Name); err != nil {
			return err
		}
		return ok(w, map[string]string{"archived": sk.Ref()})
	}
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	if err := t.Toolbox.Archive(toolbox.KindTool, t.Manifest.ID); err != nil {
		return err
	}
	return ok(w, map[string]string{"archived": t.Ref()})
}

// apiRestore brings an archived tool or skill back.
func (s *Server) apiRestore(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Toolbox string `json:"toolbox"`
		Kind    string `json:"kind"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	if req.Kind != toolbox.KindTool && req.Kind != toolbox.KindSkill {
		return errors.New("kind must be tool or skill")
	}
	ws, err := s.workspace()
	if err != nil {
		return err
	}
	tb, found := ws.Set.Toolbox(req.Toolbox)
	if !found || req.Name == "" || req.Name != filepath.Base(req.Name) || strings.HasPrefix(req.Name, ".") {
		return &httpError{code: http.StatusNotFound, msg: "no such archived item"}
	}
	if err := tb.Restore(req.Kind, req.Name); err != nil {
		return err
	}
	return ok(w, map[string]string{"restored": tb.Handle + "/" + req.Name})
}

func (s *Server) apiHealth(w http.ResponseWriter, r *http.Request) error {
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	reqs := s.checker.CheckAll(ctx, t.Manifest)
	env := workspace.Env(s.store, t)
	good := true
	for _, q := range reqs {
		good = good && q.OK()
	}
	for _, e := range env {
		good = good && (!e.Required || e.Set)
	}
	if reqs == nil {
		reqs = []doctor.Result{}
	}
	return ok(w, map[string]any{"ok": good, "requirements": reqs, "env": env})
}

// apiInstall runs the suggested install command for one requirement. The
// browser asks the user to confirm before calling it.
func (s *Server) apiInstall(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Ref, Bin string }
	if err := readJSON(r, &req); err != nil {
		return err
	}
	_, t, err := s.resolve(r, req.Ref)
	if err != nil {
		return err
	}
	var target *manifest.Requirement
	for _, q := range doctor.Requirements(t.Manifest) {
		if q.Bin == req.Bin {
			q := q
			target = &q
		}
	}
	if target == nil {
		return fmt.Errorf("%s does not require %s", t.Ref(), req.Bin)
	}
	res := s.checker.Check(r.Context(), *target)
	if res.Install == nil {
		return fmt.Errorf("no install command for %s on this machine; install it by hand", req.Bin)
	}
	if res.Install.Command[0] == "sudo" {
		return fmt.Errorf("this install needs sudo; run it in a terminal: %s", res.Install)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, res.Install.Command[0], res.Install.Command[1:]...)
	out, runErr := cmd.CombinedOutput()
	if len(out) > 200<<10 {
		out = out[len(out)-200<<10:]
	}
	after := s.checker.Check(r.Context(), *target)
	return ok(w, map[string]any{"command": res.Install.String(), "output": string(out),
		"ok": runErr == nil && after.OK(), "error": errString(runErr), "result": after})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Server) declaredSecret(r *http.Request, ref, name string) (*toolbox.Tool, error) {
	_, t, err := s.resolve(r, ref)
	if err != nil {
		return nil, err
	}
	for _, v := range t.Manifest.Env {
		if v.Name == name && v.Secret {
			return t, nil
		}
	}
	return nil, fmt.Errorf("%s has no secret env %s", t.Ref(), name)
}

func (s *Server) apiSetSecret(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Ref, Name, Value string }
	if err := readJSON(r, &req); err != nil {
		return err
	}
	t, err := s.declaredSecret(r, req.Ref, req.Name)
	if err != nil {
		return err
	}
	if req.Value == "" {
		return errors.New("enter a value")
	}
	if err := s.store.Set(t.Manifest.ID, req.Name, req.Value); err != nil {
		return err
	}
	return ok(w, map[string]any{"env": workspace.Env(s.store, t)})
}

func (s *Server) apiDeleteSecret(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	t, err := s.declaredSecret(r, q.Get("ref"), q.Get("name"))
	if err != nil {
		return err
	}
	s.store.Delete(t.Manifest.ID, q.Get("name"))
	return ok(w, map[string]any{"env": workspace.Env(s.store, t)})
}

// apiPreview shows the exact command a form would run.
func (s *Server) apiPreview(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Ref    string         `json:"ref"`
		Values map[string]any `json:"values"`
		OutDir string         `json:"out_dir"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	_, t, err := s.resolve(r, req.Ref)
	if err != nil {
		return err
	}
	out := ""
	if t.Manifest.Outputs != nil && t.Manifest.Outputs.DirFlag != "" {
		out = "<run folder>"
		if req.OutDir != "" {
			out = req.OutDir
		}
	}
	args, err := runner.BuildArgs(t.Manifest, req.Values, out)
	if err != nil {
		return ok(w, map[string]any{"error": err.Error()})
	}
	argv, _ := runner.Argv(t.Dir, t.Manifest, args)
	if t.Manifest.Entry.Path != "" {
		argv[len(t.Manifest.Entry.Interpreter)] = "./" + t.Manifest.Entry.Path
	}
	return ok(w, map[string]any{"argv": argv, "command": shellJoin(argv)})
}

func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'$&|;<>()*?[]#~`\\!{}") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

func (s *Server) apiExport(w http.ResponseWriter, r *http.Request) error {
	tmp, err := os.MkdirTemp("", "maggot-export-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var name, out string
	if isSkillReq(r) {
		_, sk, _, err := s.resolveSkill(r.URL.Query().Get("ref"))
		if err != nil {
			return err
		}
		name = pack.SkillFileName(sk.Skill)
		out = filepath.Join(tmp, name)
		if _, err := pack.PackSkill(sk.Dir, out); err != nil {
			return err
		}
	} else {
		_, t, err := s.resolve(r, "")
		if err != nil {
			return err
		}
		name = pack.FileName(t.Manifest)
		out = filepath.Join(tmp, name)
		if _, err := pack.Pack(t.Dir, out); err != nil {
			return err
		}
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Content-Type", "application/zip")
	http.ServeFile(w, r, out)
	return nil
}

// apiImportStage accepts an uploaded archive (multipart "file") or a JSON
// {"path": "..."} folder/archive on disk, and validates it.
func (s *Server) apiImportStage(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.workspace()
	if err != nil {
		return err
	}
	tb, err := ws.Target()
	if name := r.URL.Query().Get("toolbox"); name != "" {
		var found bool
		if tb, found = ws.Set.Toolbox(name); !found {
			return fmt.Errorf("no toolbox %q", name)
		}
		err = nil
	}
	if err != nil {
		return err
	}
	src := ""
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		f, hdr, err := r.FormFile("file")
		if err != nil {
			return errors.New("attach a .maggot or .zip file")
		}
		defer f.Close()
		dir, err := os.MkdirTemp("", "maggot-import-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		src = filepath.Join(dir, filepath.Base(hdr.Filename))
		dst, err := os.Create(src)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, io.LimitReader(f, pack.MaxTotalBytes))
		dst.Close()
		if err != nil {
			return err
		}
	} else {
		var req struct{ Path string }
		if err := readJSON(r, &req); err != nil {
			return err
		}
		src = config.ExpandHome(req.Path)
	}
	st, err := pack.Stage(src, tb.ToolsPath())
	if err != nil {
		return err
	}
	token := randomHex(8)
	s.mu.Lock()
	s.staged[token] = &stagedImport{staged: st, toolbox: tb.Handle, created: time.Now()}
	s.mu.Unlock()
	return ok(w, map[string]any{"token": token, "toolbox": tb.Handle, "preview": st.Preview, "needs_force": st.NeedsForce()})
}

func (s *Server) takeStaged(token string) (*stagedImport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, found := s.staged[token]
	if !found {
		return nil, &httpError{code: http.StatusNotFound, msg: "import expired; choose the file again"}
	}
	delete(s.staged, token)
	return st, nil
}

func (s *Server) apiImportCommit(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Force bool }
	readJSON(r, &req)
	st, err := s.takeStaged(r.PathValue("token"))
	if err != nil {
		return err
	}
	if st.staged.NeedsForce() && !req.Force {
		st.staged.Discard()
		return fmt.Errorf("%s %s is already installed; confirm to %s", st.staged.ID, st.staged.Existing, st.staged.Action)
	}
	if err := st.staged.Commit(); err != nil {
		st.staged.Discard()
		return err
	}
	return ok(w, map[string]string{"ref": st.toolbox + "/" + st.staged.ID, "action": st.staged.Action, "kind": st.staged.Kind})
}

func (s *Server) apiImportDiscard(w http.ResponseWriter, r *http.Request) error {
	st, err := s.takeStaged(r.PathValue("token"))
	if err != nil {
		return err
	}
	st.staged.Discard()
	return ok(w, map[string]bool{"ok": true})
}

// apiUpload saves dropped files under <data>/uploads/<id>/ and returns their
// paths, so they can be used as file inputs.
func (s *Server) apiUpload(w http.ResponseWriter, r *http.Request) error {
	mr, err := r.MultipartReader()
	if err != nil {
		return errors.New("send files as multipart/form-data")
	}
	dir := filepath.Join(s.data, "uploads", time.Now().Format("20060102-150405")+"-"+randomHex(3))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	paths := []string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Base(filepath.Clean("/" + part.FileName()))
		if part.FileName() == "" || name == "/" || name == "." {
			continue
		}
		p := filepath.Join(dir, name)
		for i := 2; ; i++ { // same name twice in one drop
			if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
				break
			}
			ext := filepath.Ext(name)
			p = filepath.Join(dir, strings.TrimSuffix(name, ext)+"-"+strconv.Itoa(i)+ext)
		}
		f, err := os.Create(p)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, part)
		f.Close()
		if err != nil {
			return err
		}
		paths = append(paths, p)
	}
	return ok(w, map[string]any{"paths": paths})
}
