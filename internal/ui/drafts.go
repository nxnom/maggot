package ui

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/toolbox"
)

// Drafts are tools the user can try before importing them:
//   - every tool folder in the drafts folder (ref "draft/<id>"), where
//     `maggot new` creates tools by default;
//   - folders added with "Try a tool folder" or `maggot try` (ref "try/<id>"),
//     kept in memory only and never written to the config.
const (
	draftHandle = "draft"
	tryHandle   = "try"
)

type draftInfo struct {
	Ref         string `json:"ref"`
	Kind        string `json:"kind"` // draft or try
	Path        string `json:"path"`
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
	// Of is the installed tool this draft is a new version of (same id).
	Of *toolLink `json:"of,omitempty"`

	tool *toolbox.Tool
}

func isDraftRef(ref string) bool {
	return strings.HasPrefix(ref, draftHandle+"/") || strings.HasPrefix(ref, tryHandle+"/")
}

// AddTry registers a folder to try. It returns the tool's ref.
func (s *Server) AddTry(path string) (string, error) {
	abs, err := filepath.Abs(config.ExpandHome(strings.TrimSpace(path)))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(abs, manifest.FileName)); err != nil {
		return "", fmt.Errorf("%s has no %s", abs, manifest.FileName)
	}
	m, err := manifest.Load(abs)
	if err != nil {
		return "", err
	}
	if drafts, _ := config.DraftsDir(); filepath.Dir(abs) == drafts {
		return draftHandle + "/" + m.ID, nil // already listed as a draft
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// One folder per tool id: trying another folder of the same tool replaces it.
	kept := s.tried[:0]
	for _, p := range s.tried {
		if p != abs {
			if pm, err := manifest.Load(p); err != nil || pm.ID != m.ID {
				kept = append(kept, p)
			}
		}
	}
	s.tried = append(kept, abs)
	return tryHandle + "/" + m.ID, nil
}

func (s *Server) forgetTry(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.tried[:0]
	for _, p := range s.tried {
		if p != path {
			kept = append(kept, p)
		}
	}
	s.tried = kept
}

// drafts lists the drafts, loading each manifest fresh.
func (s *Server) drafts() []draftInfo {
	var out []draftInfo
	load := func(kind, dir string) {
		d := draftInfo{Kind: kind, Path: dir}
		m, err := manifest.Load(dir)
		if err != nil {
			d.Error = err.Error()
			d.ID = filepath.Base(dir)
		} else {
			d.ID, d.Name, d.Version, d.Description = m.ID, m.Name, m.Version, m.Description
			handle := draftHandle
			if kind == tryHandle {
				handle = tryHandle
			}
			d.tool = &toolbox.Tool{Toolbox: &toolbox.Toolbox{Handle: handle, Path: filepath.Dir(dir)}, Dir: dir, Manifest: m}
		}
		d.Ref = kind + "/" + d.ID
		out = append(out, d)
	}
	if dir, err := config.DraftsDir(); err == nil {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				if _, err := os.Stat(filepath.Join(dir, e.Name(), manifest.FileName)); err == nil {
					load(draftHandle, filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	s.mu.Lock()
	tried := append([]string{}, s.tried...)
	s.mu.Unlock()
	for _, p := range tried {
		load(tryHandle, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func (s *Server) draft(ref string) (*draftInfo, error) {
	for _, d := range s.drafts() {
		if d.Ref == ref {
			if d.tool == nil {
				return nil, &httpError{code: http.StatusUnprocessableEntity, msg: d.Error}
			}
			return &d, nil
		}
	}
	return nil, &httpError{code: http.StatusNotFound, msg: "no draft " + ref}
}

func (s *Server) apiAddDraft(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Path string }
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if req.Path == "" {
		return errors.New("enter the tool folder path")
	}
	ref, err := s.AddTry(req.Path)
	if err != nil {
		return err
	}
	return ok(w, map[string]string{"ref": ref})
}

// apiRemoveDraft forgets a tried folder, or deletes a folder in the drafts
// folder (the UI asks first). Folders elsewhere are never deleted.
func (s *Server) apiRemoveDraft(w http.ResponseWriter, r *http.Request) error {
	ref := r.URL.Query().Get("ref")
	if isSkillReq(r) {
		for _, d := range s.skillDrafts() {
			if d.Ref == ref {
				if err := os.RemoveAll(d.Path); err != nil {
					return err
				}
				return ok(w, map[string]string{"deleted": d.Path})
			}
		}
		return &httpError{code: http.StatusNotFound, msg: "no skill draft " + ref}
	}
	for _, d := range s.drafts() {
		if d.Ref != ref {
			continue
		}
		if d.Kind == tryHandle {
			s.forgetTry(d.Path)
			return ok(w, map[string]string{"removed": ref})
		}
		if err := os.RemoveAll(d.Path); err != nil {
			return err
		}
		return ok(w, map[string]string{"deleted": d.Path})
	}
	return &httpError{code: http.StatusNotFound, msg: "no draft " + ref}
}
