// Package toolbox finds tools and skills inside toolbox folders.
//
// A toolbox is any folder with a maggot.json file and a tools/ folder.
// Every tools/<id>/ folder with a tool.json is one tool. Every
// skills/<name>/ folder with a SKILL.md is one agent skill.
package toolbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/manifest"
)

const (
	FileName  = "maggot.json"
	ToolsDir  = "tools"
	SkillsDir = "skills"
)

// Meta is the content of maggot.json.
type Meta struct {
	Schema      int    `json:"schema"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Toolbox is an opened toolbox folder. Handle is the short name used in
// tool references like "team/image-resize".
type Toolbox struct {
	Handle string
	Path   string
	Meta   Meta
}

type Tool struct {
	Toolbox  *Toolbox
	Dir      string
	Manifest *manifest.Manifest
}

// Ref is the fully qualified reference "<toolbox>/<id>".
func (t *Tool) Ref() string { return t.Toolbox.Handle + "/" + t.Manifest.ID }

// Skill is an agent skill in a toolbox.
type Skill struct {
	*agentskill.Skill
	Toolbox *Toolbox
}

// Ref is the fully qualified reference "<toolbox>/<name>".
func (s *Skill) Ref() string { return s.Toolbox.Handle + "/" + s.Meta.Name }

// Problem is a tool folder that could not be loaded.
type Problem struct {
	Toolbox string `json:"toolbox"`
	Path    string `json:"path"`
	Error   string `json:"error"`
}

// Init creates maggot.json and tools/ in dir. It fails if maggot.json exists.
func Init(dir, name string) (*Toolbox, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, ToolsDir), 0o755); err != nil {
		return nil, err
	}
	meta := Meta{Schema: 1, Name: name}
	data, _ := json.MarshalIndent(meta, "", "  ")
	f, err := os.OpenFile(filepath.Join(abs, FileName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%s already has a %s", abs, FileName)
	}
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return &Toolbox{Handle: name, Path: abs, Meta: meta}, nil
}

// Open reads maggot.json in dir.
func Open(dir, handle string) (*Toolbox, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s is not a toolbox (no %s; create one with `maggot toolbox init`)", abs, FileName)
	}
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(abs, FileName), err)
	}
	if meta.Schema != 1 {
		return nil, fmt.Errorf("%s: unsupported schema %d", filepath.Join(abs, FileName), meta.Schema)
	}
	return &Toolbox{Handle: handle, Path: abs, Meta: meta}, nil
}

// FindUp walks from start up to the filesystem root looking for maggot.json.
func FindUp(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, FileName)); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// ToolsPath is the folder that holds the tools.
func (tb *Toolbox) ToolsPath() string { return filepath.Join(tb.Path, ToolsDir) }

// SkillsPath is the folder that holds the skills.
func (tb *Toolbox) SkillsPath() string { return filepath.Join(tb.Path, SkillsDir) }

// Skills loads every skill. Broken skills are returned as problems.
func (tb *Toolbox) Skills() ([]*Skill, []Problem) {
	entries, err := os.ReadDir(tb.SkillsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []Problem{{Toolbox: tb.Handle, Path: tb.SkillsPath(), Error: err.Error()}}
	}
	var skills []*Skill
	var problems []Problem
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(tb.SkillsPath(), e.Name())
		if !agentskill.Exists(dir) {
			continue
		}
		sk, err := agentskill.LoadNamed(dir)
		if err != nil {
			problems = append(problems, Problem{Toolbox: tb.Handle, Path: dir, Error: err.Error()})
			continue
		}
		skills = append(skills, &Skill{Skill: sk, Toolbox: tb})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Meta.Name < skills[j].Meta.Name })
	return skills, problems
}

// Tools loads every tool. Broken tools are returned as problems so one bad
// folder does not hide the rest.
func (tb *Toolbox) Tools() ([]*Tool, []Problem) {
	entries, err := os.ReadDir(tb.ToolsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []Problem{{Toolbox: tb.Handle, Path: tb.ToolsPath(), Error: err.Error()}}
	}
	var tools []*Tool
	var problems []Problem
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(tb.ToolsPath(), e.Name())
		if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		m, err := manifest.Load(dir)
		if err != nil {
			problems = append(problems, Problem{Toolbox: tb.Handle, Path: dir, Error: err.Error()})
			continue
		}
		if m.ID != e.Name() {
			problems = append(problems, Problem{Toolbox: tb.Handle, Path: dir,
				Error: fmt.Sprintf("folder name %q must match tool id %q", e.Name(), m.ID)})
			continue
		}
		tools = append(tools, &Tool{Toolbox: tb, Dir: dir, Manifest: m})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Manifest.ID < tools[j].Manifest.ID })
	return tools, problems
}

// Set is the group of toolboxes maggot works with in one command.
type Set struct {
	Toolboxes     []*Toolbox
	tools         []*Tool
	problems      []Problem
	skills        []*Skill
	skillProblems []Problem
	loaded        bool
}

func (s *Set) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	for _, tb := range s.Toolboxes {
		tools, problems := tb.Tools()
		s.tools = append(s.tools, tools...)
		s.problems = append(s.problems, problems...)
		skills, problems := tb.Skills()
		s.skills = append(s.skills, skills...)
		s.skillProblems = append(s.skillProblems, problems...)
	}
}

// Skills returns skills from all toolboxes, in toolbox order.
func (s *Set) Skills() ([]*Skill, []Problem) {
	s.load()
	return s.skills, s.skillProblems
}

// ResolveSkill finds a skill by "name" or "toolbox/name".
func (s *Set) ResolveSkill(ref string) (*Skill, error) {
	s.load()
	handle, name, qualified := strings.Cut(ref, "/")
	if !qualified {
		name, handle = handle, ""
	}
	var matches []*Skill
	for _, sk := range s.skills {
		if sk.Meta.Name == name && (handle == "" || sk.Toolbox.Handle == handle) {
			matches = append(matches, sk)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		for _, p := range s.skillProblems {
			if filepath.Base(p.Path) == name {
				return nil, fmt.Errorf("skill %q is broken: %s", ref, p.Error)
			}
		}
		var similar []string
		for _, sk := range s.skills {
			if strings.Contains(sk.Meta.Name, name) || strings.Contains(name, sk.Meta.Name) {
				similar = append(similar, sk.Ref())
			}
		}
		msg := fmt.Sprintf("no skill %q", ref)
		if len(similar) > 0 {
			msg += "; did you mean: " + strings.Join(similar, ", ")
		}
		return nil, errors.New(msg + " (see `maggot skill list`)")
	default:
		refs := make([]string, len(matches))
		for i, m := range matches {
			refs[i] = m.Ref()
		}
		return nil, fmt.Errorf("skill %q is in more than one toolbox; use one of: %s", ref, strings.Join(refs, ", "))
	}
}

// Tools returns tools from all toolboxes, in toolbox order.
func (s *Set) Tools() ([]*Tool, []Problem) {
	s.load()
	return s.tools, s.problems
}

// Toolbox returns the toolbox with the given handle.
func (s *Set) Toolbox(handle string) (*Toolbox, bool) {
	for _, tb := range s.Toolboxes {
		if tb.Handle == handle {
			return tb, true
		}
	}
	return nil, false
}

// Resolve finds a tool by "id" or "toolbox/id". A bare id that exists in more
// than one toolbox is an error that lists the choices.
func (s *Set) Resolve(ref string) (*Tool, error) {
	s.load()
	handle, id, qualified := strings.Cut(ref, "/")
	if !qualified {
		id, handle = handle, ""
	}
	var matches []*Tool
	for _, t := range s.tools {
		if t.Manifest.ID == id && (handle == "" || t.Toolbox.Handle == handle) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		for _, p := range s.problems {
			if filepath.Base(p.Path) == id {
				return nil, fmt.Errorf("tool %q is broken: %s", ref, p.Error)
			}
		}
		msg := fmt.Sprintf("no tool %q", ref)
		if similar := s.similar(id); len(similar) > 0 {
			msg += "; did you mean: " + strings.Join(similar, ", ")
		}
		return nil, errors.New(msg + " (see `maggot list`)")
	default:
		refs := make([]string, len(matches))
		for i, m := range matches {
			refs[i] = m.Ref()
		}
		return nil, fmt.Errorf("%q is in more than one toolbox; use one of: %s", ref, strings.Join(refs, ", "))
	}
}

func (s *Set) similar(id string) []string {
	var out []string
	for _, t := range s.tools {
		if strings.Contains(t.Manifest.ID, id) || strings.Contains(id, t.Manifest.ID) {
			out = append(out, t.Ref())
		}
	}
	return out
}
