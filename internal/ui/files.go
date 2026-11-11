package ui

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/editor"
	"github.com/nxnom/maggot/internal/manifest"
)

// Folders not worth listing file by file.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "__pycache__": true, ".venv": true, "venv": true, ".mypy_cache": true}

const maxViewBytes = 512 << 10

type toolFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Dir     bool   `json:"dir,omitempty"`     // a skipped folder
	Skipped bool   `json:"skipped,omitempty"` // contents not listed
}

// apiToolFiles lists a tool's files for the read-only viewer. tool.json and
// the entry file come first.
func (s *Server) apiToolFiles(w http.ResponseWriter, r *http.Request) error {
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	var files []toolFile
	filepath.WalkDir(t.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == t.Dir || len(files) >= 1000 {
			return nil
		}
		rel, _ := filepath.Rel(t.Dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[d.Name()] {
				files = append(files, toolFile{Path: rel + "/", Dir: true, Skipped: true})
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			files = append(files, toolFile{Path: rel, Size: info.Size()})
		}
		return nil
	})
	rank := func(f toolFile) int {
		switch {
		case f.Path == manifest.FileName:
			return 0
		case f.Path == t.Manifest.Entry.Path:
			return 1
		case f.Path == "README.md":
			return 2
		case strings.HasPrefix(f.Path, "tests/"):
			return 4
		}
		return 3
	}
	sort.SliceStable(files, func(i, j int) bool {
		if ri, rj := rank(files[i]), rank(files[j]); ri != rj {
			return ri < rj
		}
		return files[i].Path < files[j].Path
	})
	if files == nil {
		files = []toolFile{}
	}
	return ok(w, map[string]any{"dir": t.Dir, "files": files})
}

// apiToolFile returns one file's text. Binary and very large files are
// reported instead of returned.
func (s *Server) apiToolFile(w http.ResponseWriter, r *http.Request) error {
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	p, err := insideDir(t.Dir, r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return os.ErrNotExist
	}
	data, err := io.ReadAll(io.LimitReader(f, maxViewBytes))
	if err != nil {
		return err
	}
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(trimPartialRune(head)) {
		return ok(w, map[string]any{"binary": true, "size": info.Size()})
	}
	return ok(w, map[string]any{"text": string(data), "size": info.Size(), "truncated": info.Size() > maxViewBytes})
}

// trimPartialRune drops a UTF-8 sequence cut off at the end of b.
func trimPartialRune(b []byte) []byte {
	for i := 0; i < utf8.UTFMax && len(b) > 0; i++ {
		if r, size := utf8.DecodeLastRune(b); r != utf8.RuneError || size != 1 {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}

// apiOpenTool opens the tool folder in the file manager or the code editor.
func (s *Server) apiOpenTool(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Ref, How string }
	if err := readJSON(r, &req); err != nil {
		return err
	}
	_, t, err := s.resolve(r, req.Ref)
	if err != nil {
		return err
	}
	switch req.How {
	case "folder":
		return okOr(w, s.open(t.Dir, false))
	case "editor":
		ed, err := s.editor()
		if err != nil {
			return err
		}
		return okOr(w, ed.Open(t.Dir))
	}
	return errors.New(`how must be "folder" or "editor"`)
}

func (s *Server) editor() (editor.Editor, error) {
	cfg, err := config.Load()
	if err != nil {
		return editor.Editor{}, err
	}
	return editor.Find(cfg.Editor, nil)
}

func okOr(w http.ResponseWriter, err error) error {
	if err != nil {
		return err
	}
	return ok(w, map[string]bool{"ok": true})
}

// editorName is shown on the "Open in …" button ("" when none is found).
func (s *Server) editorName() string {
	ed, err := s.editor()
	if err != nil {
		return ""
	}
	return ed.Name
}
