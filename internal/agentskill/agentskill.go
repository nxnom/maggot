// Package agentskill reads skills in the Agent Skills format
// (https://agentskills.io): a folder with a SKILL.md file that starts with
// YAML frontmatter (name, description, ...) followed by markdown
// instructions, plus optional scripts/, references/ and assets/ folders.
//
// maggot keeps its own fields under the standard "metadata" map, so the
// files stay valid for every agent that reads skills:
//
//	metadata:
//	  version: 1.0.0
//	  maggot-uses: voxcpm-tts depth-map      # maggot tools the steps call
//	  maggot-requires: ffmpeg node           # programs that must be on PATH
//	  maggot-env: STABILITY_API_KEY          # secrets the scripts need
package agentskill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the skill file inside a skill folder.
const FileName = "SKILL.md"

// Reserved is the name of maggot's own skill; toolbox skills cannot use it.
const Reserved = "maggot"

// Limits from the Agent Skills specification.
const (
	MaxNameLen          = 64
	MaxDescriptionLen   = 1024
	MaxCompatibilityLen = 500
)

// Metadata keys maggot reads.
const (
	KeyVersion  = "version"
	KeyUses     = "maggot-uses"
	KeyRequires = "maggot-requires"
	KeyEnv      = "maggot-env"
)

// Meta is the SKILL.md frontmatter. Unknown fields are ignored.
type Meta struct {
	Name          string         `yaml:"name" json:"name"`
	Description   string         `yaml:"description" json:"description"`
	License       string         `yaml:"license,omitempty" json:"license,omitempty"`
	Compatibility string         `yaml:"compatibility,omitempty" json:"compatibility,omitempty"`
	Metadata      map[string]any `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	// Version outside metadata is not in the spec, but some skills have it.
	Version string `yaml:"version,omitempty" json:"-"`
}

// Skill is a loaded skill folder.
type Skill struct {
	Dir  string `json:"dir"`
	Meta Meta   `json:"meta"`
	// Body is the markdown after the frontmatter.
	Body string `json:"-"`
	// From metadata; empty when not set.
	Version  string   `json:"version"`
	Uses     []string `json:"uses"`
	Requires []string `json:"requires"`
	Env      []string `json:"env"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidName reports why name is not a valid skill name, or nil.
func ValidName(name string) error {
	switch {
	case name == "":
		return errors.New("name is required")
	case len(name) > MaxNameLen:
		return fmt.Errorf("name %q is longer than %d characters", name, MaxNameLen)
	case !nameRe.MatchString(name):
		return fmt.Errorf("name %q must be lowercase letters, digits and single hyphens (like \"video-short\")", name)
	case name == Reserved:
		return fmt.Errorf("name %q is used by maggot's own skill; pick another name", name)
	}
	return nil
}

// Split separates the frontmatter from the body. The file must start with a
// "---" line; the frontmatter ends at the next "---" line.
func Split(data []byte) (front, body []byte, err error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, nil, errors.New("SKILL.md must start with a --- line and YAML frontmatter (name, description)")
	}
	rest := text[4:]
	end := -1
	if strings.HasPrefix(rest, "---\n") || rest == "---" {
		end = 0
	} else if i := strings.Index(rest, "\n---\n"); i >= 0 {
		end = i + 1
	} else if strings.HasSuffix(rest, "\n---") {
		end = len(rest) - 3
	}
	if end < 0 {
		return nil, nil, errors.New("SKILL.md frontmatter has no closing --- line")
	}
	front = []byte(rest[:end])
	body = []byte(strings.TrimPrefix(strings.TrimPrefix(rest[end:], "---"), "\n"))
	return front, body, nil
}

// Parse reads SKILL.md content without checking the folder name.
func Parse(data []byte) (*Skill, error) {
	front, body, err := Split(data)
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := yaml.Unmarshal(front, &m); err != nil {
		return nil, fmt.Errorf("SKILL.md frontmatter: %w", err)
	}
	s := &Skill{Meta: m, Body: string(body)}
	s.Version = metaString(m.Metadata[KeyVersion])
	if s.Version == "" {
		s.Version = m.Version
	}
	s.Uses = metaList(m.Metadata[KeyUses])
	s.Requires = metaList(m.Metadata[KeyRequires])
	s.Env = metaList(m.Metadata[KeyEnv])
	return s, s.validate()
}

func (s *Skill) validate() error {
	var problems []string
	m := s.Meta
	if err := ValidName(m.Name); err != nil {
		problems = append(problems, err.Error())
	}
	switch d := strings.TrimSpace(m.Description); {
	case d == "":
		problems = append(problems, "description is required: say what the skill does and when to use it")
	case len(d) > MaxDescriptionLen:
		problems = append(problems, fmt.Sprintf("description is longer than %d characters", MaxDescriptionLen))
	}
	if len(m.Compatibility) > MaxCompatibilityLen {
		problems = append(problems, fmt.Sprintf("compatibility is longer than %d characters", MaxCompatibilityLen))
	}
	for _, e := range s.Env {
		if !envRe.MatchString(e) {
			problems = append(problems, fmt.Sprintf("metadata.%s: %q is not an env var name", KeyEnv, e))
		}
	}
	if len(problems) > 0 {
		return errors.New("SKILL.md: " + strings.Join(problems, "; "))
	}
	return nil
}

var envRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Load reads dir/SKILL.md. The folder name must match the skill name, as
// the spec asks.
func Load(dir string) (*Skill, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	s, err := Parse(data)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	s.Dir = abs
	return s, nil
}

// LoadNamed is Load plus the folder name check.
func LoadNamed(dir string) (*Skill, error) {
	s, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if base := filepath.Base(s.Dir); base != s.Meta.Name {
		return nil, fmt.Errorf("folder name %q must match skill name %q", base, s.Meta.Name)
	}
	return s, nil
}

// Exists reports whether dir has a SKILL.md.
func Exists(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil && !info.IsDir()
}

func metaString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

// metaList accepts "a b", "a, b" or a YAML list. The spec says metadata
// values are strings, so the string forms are the ones to write.
func metaList(v any) []string {
	var parts []string
	switch x := v.(type) {
	case nil:
	case []any:
		for _, item := range x {
			parts = append(parts, metaString(item))
		}
	default:
		parts = strings.FieldsFunc(metaString(x), func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	}
	out := []string{}
	seen := map[string]bool{}
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// Template is a starting SKILL.md for `maggot skill new`.
func Template(name, description string) string {
	if description == "" {
		description = "TODO: what this skill does, and when an agent should use it (\"Use when ...\")."
	}
	return fmt.Sprintf(`---
name: %s
description: %s
metadata:
  version: 0.1.0
  maggot-uses: ""
  maggot-requires: ""
  maggot-env: ""
---

# %s

<!-- Instructions for the agent. Keep this file short (under ~500 lines);
     put long reference material in references/ and code in scripts/. -->

## When to use

- TODO

## Steps

1. TODO: first step. Name the exact command, for example:
   `+"`python3 scripts/example.py <input>`"+` (paths are relative to this folder).
2. TODO

## Rules

- TODO: what we learned (what works, what to avoid).
`, name, yamlScalar(description), titleCase(name))
}

// yamlScalar quotes s when plain YAML would misread it.
func yamlScalar(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%q", s)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func titleCase(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
