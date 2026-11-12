package ui

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nxnom/maggot/internal/toolbox"
)

// A draft and an installed tool with the same id are the same tool: the
// draft is a new version of it. Importing the draft replaces the installed
// tool. These helpers connect the two and show what the draft changed.

// toolLink points from a draft to its installed tool, or back.
type toolLink struct {
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Toolbox string `json:"toolbox,omitempty"`
}

// installedFor returns the installed tool with the draft's id. With the same
// id in several toolboxes, the one in the default toolbox wins.
func installedFor(id string, tools []*toolbox.Tool, defaultBox string) *toolbox.Tool {
	var found *toolbox.Tool
	for _, t := range tools {
		if t.Manifest.ID != id {
			continue
		}
		if found == nil || t.Toolbox.Handle == defaultBox {
			found = t
		}
	}
	return found
}

func linkTo(t *toolbox.Tool) *toolLink {
	return &toolLink{Ref: t.Ref(), Name: t.Manifest.Name, Version: t.Manifest.Version, Toolbox: t.Toolbox.Handle}
}

func linkToDraft(d draftInfo) *toolLink {
	return &toolLink{Ref: d.Ref, Name: d.Name, Version: d.Version}
}

// linkDrafts fills Of on drafts that change an installed tool.
func linkDrafts(drafts []draftInfo, tools []*toolbox.Tool, defaultBox string) {
	for i := range drafts {
		if drafts[i].tool == nil {
			continue
		}
		if t := installedFor(drafts[i].ID, tools, defaultBox); t != nil {
			drafts[i].Of = linkTo(t)
		}
	}
}

// draftFor returns the draft of an installed tool, if there is one.
func draftFor(id string, drafts []draftInfo) *draftInfo {
	for i := range drafts {
		if drafts[i].tool != nil && drafts[i].ID == id {
			return &drafts[i]
		}
	}
	return nil
}

type fileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added, changed, removed
}

const maxCompareFiles = 2000

// fileHashes maps each file under dir (slash paths) to its sha256.
func fileHashes(dir string) (map[string][32]byte, error) {
	out := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if len(out) >= maxCompareFiles {
			return errors.New("too many files to compare")
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = sha256.Sum256(data)
		return nil
	})
	return out, err
}

// compareDirs lists the files that differ between an old and a new folder.
func compareDirs(oldDir, newDir string) ([]fileChange, error) {
	a, err := fileHashes(oldDir)
	if err != nil {
		return nil, err
	}
	b, err := fileHashes(newDir)
	if err != nil {
		return nil, err
	}
	changes := []fileChange{}
	for p, hb := range b {
		ha, found := a[p]
		switch {
		case !found:
			changes = append(changes, fileChange{p, "added"})
		case ha != hb:
			changes = append(changes, fileChange{p, "changed"})
		}
	}
	for p := range a {
		if _, found := b[p]; !found {
			changes = append(changes, fileChange{p, "removed"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// draftAndInstalled resolves a draft ref and the installed tool it changes.
func (s *Server) draftAndInstalled(r *http.Request) (*draftInfo, *toolbox.Tool, error) {
	ws, err := s.workspace()
	if err != nil {
		return nil, nil, err
	}
	ref := r.URL.Query().Get("ref")
	if !isDraftRef(ref) {
		return nil, nil, errors.New("not a draft: " + ref)
	}
	d, err := s.draft(ref)
	if err != nil {
		return nil, nil, err
	}
	tools, _ := ws.Set.Tools()
	t := installedFor(d.ID, tools, ws.Config.Default)
	if t == nil {
		return nil, nil, &httpError{code: http.StatusNotFound, msg: "no installed tool with id " + d.ID}
	}
	return d, t, nil
}

// apiChanges lists the files a draft changed compared with the installed tool.
func (s *Server) apiChanges(w http.ResponseWriter, r *http.Request) error {
	draftDir, of, installedDir, err := s.changePair(r)
	if err != nil {
		return err
	}
	changes, err := compareDirs(installedDir, draftDir)
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"of": of, "files": changes})
}

// changePair returns the draft folder, the installed tool or skill it
// changes, and that one's folder.
func (s *Server) changePair(r *http.Request) (string, *toolLink, string, error) {
	if isSkillReq(r) {
		return s.skillDraftAndInstalled(r.URL.Query().Get("ref"))
	}
	d, t, err := s.draftAndInstalled(r)
	if err != nil {
		return "", nil, "", err
	}
	return d.Path, linkTo(t), t.Dir, nil
}

// apiDiff returns a line diff of one file: installed version → draft.
func (s *Server) apiDiff(w http.ResponseWriter, r *http.Request) error {
	draftDir, _, installedDir, err := s.changePair(r)
	if err != nil {
		return err
	}
	rel := r.URL.Query().Get("path")
	read := func(dir string) ([]byte, error) {
		p, err := insideDir(dir, rel)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil // added or removed: diff against nothing
		}
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil // added or removed: diff against nothing
		}
		return data, err
	}
	a, err := read(installedDir)
	if err != nil {
		return err
	}
	b, err := read(draftDir)
	if err != nil {
		return err
	}
	if isBinary(a) || isBinary(b) {
		return ok(w, map[string]any{"binary": true, "old_size": len(a), "new_size": len(b)})
	}
	lines, tooBig := diffLines(fileLines(a), fileLines(b), 3)
	if tooBig {
		return ok(w, map[string]any{"too_large": true})
	}
	return ok(w, map[string]any{"lines": lines})
}

func isBinary(b []byte) bool {
	head := b
	if len(head) > 8192 {
		head = head[:8192]
	}
	return bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(trimPartialRune(head))
}

func fileLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// diffLine is one row of a diff. Op is " " (same), "-" (removed), "+"
// (added) or "~" (Skip unchanged lines left out).
type diffLine struct {
	Op   string `json:"op"`
	Text string `json:"text,omitempty"`
	Old  int    `json:"old,omitempty"` // line number in the old file
	New  int    `json:"new,omitempty"` // line number in the new file
	Skip int    `json:"skip,omitempty"`
}

const maxDiffCells = 4_000_000

// diffLines diffs two files by lines (longest common subsequence) and keeps
// context lines of unchanged text around each change.
func diffLines(a, b []string, context int) ([]diffLine, bool) {
	// Trim the common head and tail; most edits are small.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if (len(ma)+1)*(len(mb)+1) > maxDiffCells {
		return nil, true
	}
	// lcs[i][j] = LCS length of ma[i:] and mb[j:].
	lcs := make([][]int32, len(ma)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(mb)+1)
	}
	for i := len(ma) - 1; i >= 0; i-- {
		for j := len(mb) - 1; j >= 0; j-- {
			if ma[i] == mb[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var all []diffLine
	for i := 0; i < pre; i++ {
		all = append(all, diffLine{Op: " ", Text: a[i]})
	}
	i, j := 0, 0
	for i < len(ma) || j < len(mb) {
		switch {
		case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
			all = append(all, diffLine{Op: " ", Text: ma[i]})
			i++
			j++
		case i < len(ma) && (j == len(mb) || lcs[i+1][j] >= lcs[i][j+1]):
			all = append(all, diffLine{Op: "-", Text: ma[i]}) // removals first, like git
			i++
		default:
			all = append(all, diffLine{Op: "+", Text: mb[j]})
			j++
		}
	}
	for k := len(a) - suf; k < len(a); k++ {
		all = append(all, diffLine{Op: " ", Text: a[k]})
	}
	// Line numbers.
	on, nn := 0, 0
	for k := range all {
		switch all[k].Op {
		case " ":
			on++
			nn++
			all[k].Old, all[k].New = on, nn
		case "-":
			on++
			all[k].Old = on
		case "+":
			nn++
			all[k].New = nn
		}
	}
	// Keep only context lines near a change.
	keep := make([]bool, len(all))
	for k, l := range all {
		if l.Op != " " {
			for c := max(0, k-context); c <= min(len(all)-1, k+context); c++ {
				keep[c] = true
			}
		}
	}
	out := []diffLine{}
	for k := 0; k < len(all); {
		if keep[k] {
			out = append(out, all[k])
			k++
			continue
		}
		start := k
		for k < len(all) && !keep[k] {
			k++
		}
		if k-start < 3 { // hiding one or two lines saves nothing
			out = append(out, all[start:k]...)
			continue
		}
		out = append(out, diffLine{Op: "~", Skip: k - start})
	}
	return out, false
}
