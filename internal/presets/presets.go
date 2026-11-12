// Package presets stores named sets of input values for a tool.
//
// A preset lives in one of two places:
//   - personal: <data dir>/presets/<tool id>/<name>.json, only on this machine;
//   - toolbox:  <tool folder>/presets/<name>.json, shared with the tool
//     (git, exports, drafts).
//
// When both have a preset with the same name, the personal one is used,
// unless the name says which: "toolbox/narrator" or "personal/narrator".
package presets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nxnom/maggot/internal/config"
)

const (
	Personal = "personal"
	Toolbox  = "toolbox"
	// Dir is the presets folder inside a tool folder.
	Dir = "presets"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Preset is one saved set of input values.
type Preset struct {
	Name        string         `json:"name"`
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Values      map[string]any `json:"values"`
	Scope       string         `json:"scope"` // personal or toolbox (not stored)
}

type file struct {
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Values      map[string]any `json:"values"`
}

// Slug turns a label like "Narrator (calm)" into a name like "narrator-calm".
func Slug(label string) string {
	s := strings.ToLower(label)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return s
}

// ValidName reports whether name can be used as a preset name.
func ValidName(name string) bool { return namePattern.MatchString(name) }

func personalDir(toolID string) (string, error) {
	d, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "presets", toolID), nil
}

func dirFor(scope, toolID, toolDir string) (string, error) {
	switch scope {
	case Personal:
		return personalDir(toolID)
	case Toolbox:
		return filepath.Join(toolDir, Dir), nil
	}
	return "", fmt.Errorf("scope must be %q or %q", Personal, Toolbox)
}

// List returns the presets of a tool: personal first, then toolbox, each by name.
func List(toolID, toolDir string) []Preset {
	var out []Preset
	for _, scope := range []string{Personal, Toolbox} {
		dir, err := dirFor(scope, toolID, toolDir)
		if err != nil {
			continue
		}
		entries, _ := os.ReadDir(dir)
		names := []string{}
		for _, e := range entries {
			if n, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() && ValidName(n) {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			if p, err := read(filepath.Join(dir, n+".json"), n, scope, toolDir); err == nil {
				out = append(out, *p)
			}
		}
	}
	return out
}

func read(path, name, scope, toolDir string) (*Preset, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("preset %s: %w", name, err)
	}
	if f.Values == nil {
		f.Values = map[string]any{}
	}
	return &Preset{Name: name, Label: f.Label, Description: f.Description, Values: expand(f.Values, toolDir), Scope: scope}, nil
}

// Get finds a preset by "name", "personal/name" or "toolbox/name".
func Get(toolID, toolDir, ref string) (*Preset, error) {
	scopes := []string{Personal, Toolbox}
	name := ref
	if s, n, ok := strings.Cut(ref, "/"); ok {
		if s != Personal && s != Toolbox {
			return nil, fmt.Errorf("unknown preset scope %q (use personal/ or toolbox/)", s)
		}
		scopes, name = []string{s}, n
	}
	for _, scope := range scopes {
		dir, err := dirFor(scope, toolID, toolDir)
		if err != nil {
			return nil, err
		}
		p, err := read(filepath.Join(dir, name+".json"), name, scope, toolDir)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	var names []string
	for _, p := range List(toolID, toolDir) {
		names = append(names, p.Scope+"/"+p.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no preset %q; this tool has no presets yet", ref)
	}
	return nil, fmt.Errorf("no preset %q; available: %s", ref, strings.Join(names, ", "))
}

// Save writes a preset and returns warnings (for example a toolbox preset
// with a path that only exists on this machine).
func Save(toolID, toolDir string, p Preset) ([]string, error) {
	if !ValidName(p.Name) {
		return nil, fmt.Errorf("invalid preset name %q (lowercase letters, digits and -)", p.Name)
	}
	dir, err := dirFor(p.Scope, toolID, toolDir)
	if err != nil {
		return nil, err
	}
	values := p.Values
	var warnings []string
	if p.Scope == Toolbox {
		values, warnings = portable(values, toolDir)
	}
	data, _ := json.MarshalIndent(file{Label: p.Label, Description: p.Description, Values: values}, "", "  ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return warnings, config.WriteFileAtomic(filepath.Join(dir, p.Name+".json"), append(data, '\n'), 0o644)
}

// Delete removes a preset ("name", "personal/name" or "toolbox/name").
func Delete(toolID, toolDir, ref string) error {
	p, err := Get(toolID, toolDir, ref)
	if err != nil {
		return err
	}
	dir, _ := dirFor(p.Scope, toolID, toolDir)
	return os.Remove(filepath.Join(dir, p.Name+".json"))
}

// Merge returns the preset values with over on top.
func Merge(p *Preset, over map[string]any) map[string]any {
	out := map[string]any{}
	if p != nil {
		for k, v := range p.Values {
			out[k] = v
		}
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// expand replaces {tool} with the tool folder in string values.
func expand(values map[string]any, toolDir string) map[string]any {
	out := map[string]any{}
	for k, v := range values {
		out[k] = mapStrings(v, func(s string) string { return strings.ReplaceAll(s, "{tool}", toolDir) })
	}
	return out
}

// portable turns paths inside the tool folder into {tool}/... and warns
// about other absolute paths, which may not exist on other machines.
func portable(values map[string]any, toolDir string) (map[string]any, []string) {
	var warnings []string
	abs, _ := filepath.Abs(toolDir)
	out := map[string]any{}
	for k, v := range values {
		out[k] = mapStrings(v, func(s string) string {
			if abs != "" && (s == abs || strings.HasPrefix(s, abs+string(filepath.Separator))) {
				return "{tool}" + filepath.ToSlash(strings.TrimPrefix(s, abs))
			}
			if filepath.IsAbs(s) || strings.HasPrefix(s, "~/") {
				warnings = append(warnings, fmt.Sprintf("%s: %s is a path on this computer; it may not exist where the toolbox is used", k, s))
			}
			return s
		})
	}
	return out, warnings
}

func mapStrings(v any, f func(string) string) any {
	switch x := v.(type) {
	case string:
		return f(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = mapStrings(item, f)
		}
		return out
	}
	return v
}
