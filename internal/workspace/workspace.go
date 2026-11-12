// Package workspace loads what maggot works with in one session: the user
// config, the active toolboxes and their tools. The CLI and the web UI share it.
package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/secrets"
	"github.com/nxnom/maggot/internal/toolbox"
)

type Workspace struct {
	Config *config.Config
	Set    *toolbox.Set
	// Problems are registered toolboxes that could not be opened.
	Problems []toolbox.Problem
	// Selected is true when one toolbox was chosen with --toolbox / $MAGGOT_TOOLBOX.
	Selected bool
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Slug makes a toolbox handle from free text.
func Slug(s string) string {
	s = strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// HandleFor picks a toolbox handle from its maggot.json name or folder name.
func HandleFor(dir, metaName string) string {
	if h := Slug(metaName); h != "" {
		return h
	}
	if h := Slug(filepath.Base(dir)); h != "" {
		return h
	}
	return "local"
}

// Load builds the toolbox set. With a selector (a registered name or a
// path) only that toolbox is used. Otherwise every registered toolbox is
// used, plus the toolbox around cwd when it is not registered. Pass cwd ""
// to skip that.
func Load(selector, cwd string) (*Workspace, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	w := &Workspace{Config: cfg, Set: &toolbox.Set{}}
	if selector != "" {
		w.Selected = true
		if ref, ok := cfg.Find(selector); ok {
			tb, err := toolbox.Open(ref.Path, ref.Name)
			if err != nil {
				return nil, err
			}
			w.Set.Toolboxes = []*toolbox.Toolbox{tb}
			return w, nil
		}
		tb, err := toolbox.Open(config.ExpandHome(selector), "")
		if err != nil {
			return nil, fmt.Errorf("--toolbox %q is not a registered name or a toolbox folder: %w", selector, err)
		}
		tb.Handle = HandleFor(tb.Path, tb.Meta.Name)
		w.Set.Toolboxes = []*toolbox.Toolbox{tb}
		return w, nil
	}

	seen := map[string]bool{}
	for _, ref := range cfg.Toolboxes {
		tb, err := toolbox.Open(ref.Path, ref.Name)
		if err != nil {
			w.Problems = append(w.Problems, toolbox.Problem{Toolbox: ref.Name, Path: ref.Path, Error: err.Error()})
			continue
		}
		w.Set.Toolboxes = append(w.Set.Toolboxes, tb)
		seen[tb.Path] = true
	}
	if cwd != "" {
		if dir, ok := toolbox.FindUp(cwd); ok && !seen[dir] {
			if tb, err := toolbox.Open(dir, ""); err == nil {
				tb.Handle = HandleFor(tb.Path, tb.Meta.Name)
				if _, taken := w.Set.Toolbox(tb.Handle); taken {
					tb.Handle = "local"
				}
				w.Set.Toolboxes = append(w.Set.Toolboxes, tb)
			}
		}
	}
	return w, nil
}

// AllProblems merges toolbox-level, tool-level and skill-level problems.
func (w *Workspace) AllProblems() []toolbox.Problem {
	_, p := w.Set.Tools()
	_, sp := w.Set.Skills()
	return append(append(append([]toolbox.Problem{}, w.Problems...), p...), sp...)
}

// Target is where new or imported tools go: the selected toolbox, otherwise
// the default registered toolbox.
func (w *Workspace) Target() (*toolbox.Toolbox, error) {
	if w.Selected {
		return w.Set.Toolboxes[0], nil
	}
	if w.Config.Default == "" {
		return nil, fmt.Errorf("no default toolbox; create one with `maggot toolbox init <path>` or pass --toolbox")
	}
	tb, ok := w.Set.Toolbox(w.Config.Default)
	if !ok {
		return nil, fmt.Errorf("default toolbox %q is missing", w.Config.Default)
	}
	return tb, nil
}

// EnvStatus says whether a value is available for an env var a tool declares.
type EnvStatus struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret"`
	Required    bool   `json:"required"`
	Set         bool   `json:"set"`
	// Source is "env" (already in the environment), "store" (saved secret) or "".
	Source string `json:"source,omitempty"`
}

// Env reports every declared env var. A value in the environment wins over a
// stored secret. store may be nil.
func Env(store secrets.Store, t *toolbox.Tool) []EnvStatus {
	out := []EnvStatus{}
	for _, v := range t.Manifest.Env {
		s := EnvStatus{Name: v.Name, Description: v.Description, Secret: v.Secret, Required: v.Required}
		if _, ok := os.LookupEnv(v.Name); ok {
			s.Set, s.Source = true, "env"
		} else if v.Secret && store != nil {
			if _, ok, _ := store.Get(t.Manifest.ID, v.Name); ok {
				s.Set, s.Source = true, "store"
			}
		}
		out = append(out, s)
	}
	return out
}

// SecretEnv returns NAME=value pairs for stored secrets not already set in
// the environment.
func SecretEnv(store secrets.Store, t *toolbox.Tool) ([]string, error) {
	var out []string
	for _, v := range t.Manifest.Env {
		if !v.Secret {
			continue
		}
		if _, ok := os.LookupEnv(v.Name); ok {
			continue
		}
		if store == nil {
			continue
		}
		val, ok, err := store.Get(t.Manifest.ID, v.Name)
		if err != nil {
			return nil, fmt.Errorf("read secret %s: %w", v.Name, err)
		}
		if ok {
			out = append(out, v.Name+"="+val)
		}
	}
	return out, nil
}

// Missing is the quick pre-run check: binaries not on PATH and required env
// vars without a value. lookPath may be nil (exec.LookPath).
func Missing(t *toolbox.Tool, store secrets.Store, lookPath func(string) (string, error)) []string {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	var missing []string
	for _, r := range doctor.Requirements(t.Manifest) {
		if _, err := lookPath(r.Bin); err != nil {
			missing = append(missing, "binary "+r.Bin)
		}
	}
	for _, s := range Env(store, t) {
		if s.Required && !s.Set {
			missing = append(missing, "env "+s.Name)
		}
	}
	return missing
}
