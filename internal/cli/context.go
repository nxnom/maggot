package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/toolbox"
)

// env holds what most commands need: the config and the active toolbox set.
type env struct {
	cfg      *config.Config
	set      *toolbox.Set
	problems []toolbox.Problem
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// handleFor picks a toolbox handle from its toolbox.json name or folder name.
func handleFor(dir, metaName string) string {
	if h := slug(metaName); h != "" {
		return h
	}
	if h := slug(filepath.Base(dir)); h != "" {
		return h
	}
	return "local"
}

// loadEnv builds the toolbox set. Selection order:
//  1. --toolbox flag or $MAGGOT_TOOLBOX (a registered name or a path) → only that toolbox
//  2. otherwise every registered toolbox, plus the toolbox around the current
//     directory when it is not registered (handy inside a git checkout).
func (a *app) loadEnv() (*env, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	e := &env{cfg: cfg, set: &toolbox.Set{}}

	sel := a.toolboxFlag
	if sel == "" {
		sel = os.Getenv("MAGGOT_TOOLBOX")
	}
	if sel != "" {
		if ref, ok := cfg.Find(sel); ok {
			tb, err := toolbox.Open(ref.Path, ref.Name)
			if err != nil {
				return nil, err
			}
			e.set.Toolboxes = []*toolbox.Toolbox{tb}
			return e, nil
		}
		tb, err := toolbox.Open(sel, "")
		if err != nil {
			return nil, fmt.Errorf("--toolbox %q is not a registered name or a toolbox folder: %w", sel, err)
		}
		tb.Handle = handleFor(tb.Path, tb.Meta.Name)
		e.set.Toolboxes = []*toolbox.Toolbox{tb}
		return e, nil
	}

	seen := map[string]bool{}
	for _, ref := range cfg.Toolboxes {
		tb, err := toolbox.Open(ref.Path, ref.Name)
		if err != nil {
			e.problems = append(e.problems, toolbox.Problem{Toolbox: ref.Name, Path: ref.Path, Error: err.Error()})
			continue
		}
		e.set.Toolboxes = append(e.set.Toolboxes, tb)
		seen[tb.Path] = true
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir, ok := toolbox.FindUp(cwd); ok && !seen[dir] {
			if tb, err := toolbox.Open(dir, ""); err == nil {
				tb.Handle = handleFor(tb.Path, tb.Meta.Name)
				if _, taken := e.set.Toolbox(tb.Handle); taken {
					tb.Handle = "local"
				}
				e.set.Toolboxes = append(e.set.Toolboxes, tb)
			}
		}
	}
	return e, nil
}

// allProblems merges toolbox-level and tool-level problems.
func (e *env) allProblems() []toolbox.Problem {
	_, p := e.set.Tools()
	return append(append([]toolbox.Problem{}, e.problems...), p...)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func (a *app) warnProblems(problems []toolbox.Problem) {
	for _, p := range problems {
		fmt.Fprintf(a.stderr, "warning: %s: %s\n", p.Path, firstLine(p.Error))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
