// Package skill installs maggot's single agent skill (SKILL.md) for coding
// agents such as Claude Code and Codex.
package skill

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed SKILL.md
var Text string

// Name is the skill folder name.
const Name = "maggot"

// targets maps an agent to its skills folder, relative to the home folder
// (user scope) or the project folder (project scope).
var targets = map[string]string{
	"claude": filepath.Join(".claude", "skills"),
	"codex":  filepath.Join(".codex", "skills"),
	"agents": filepath.Join(".agents", "skills"),
}

// Agents lists the supported agent names.
func Agents() []string {
	out := make([]string, 0, len(targets))
	for k := range targets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Path returns where the skill file goes for agent under base (home or project).
func Path(base, agent string) (string, error) {
	rel, ok := targets[agent]
	if !ok {
		return "", fmt.Errorf("unknown agent %q (choose: %s)", agent, strings.Join(Agents(), ", "))
	}
	return filepath.Join(base, rel, Name, "SKILL.md"), nil
}

// Install writes SKILL.md for agent under base and returns the path and
// whether the file changed.
func Install(base, agent string) (string, bool, error) {
	p, err := Path(base, agent)
	if err != nil {
		return "", false, err
	}
	if old, err := os.ReadFile(p); err == nil && string(old) == Text {
		return p, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", false, err
	}
	return p, true, os.WriteFile(p, []byte(Text), 0o644)
}

// Uninstall removes the skill folder for agent under base.
func Uninstall(base, agent string) (string, error) {
	p, err := Path(base, agent)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(p)
	if _, err := os.Stat(dir); err != nil {
		return dir, nil
	}
	return dir, os.RemoveAll(dir)
}

// Toolbox skills can be installed into an agent's skills folder too, so the
// agent finds them on its own. maggot links the folder (a symlink, so edits
// in the toolbox show up at once) or, where links do not work, copies it and
// leaves a marker file to know the copy is its own.
const (
	StateNone   = "none"   // not installed for this agent
	StateLinked = "linked" // a symlink to this skill's folder
	StateCopied = "copied" // a copy maggot made
	StateOther  = "other"  // a different skill with the same name
)

// SourceMarker is written into copies; it holds the source folder.
const SourceMarker = ".maggot-source"

// Dir is where a skill named name goes for agent under base.
func Dir(base, agent, name string) (string, error) {
	rel, ok := targets[agent]
	if !ok {
		return "", fmt.Errorf("unknown agent %q (choose: %s)", agent, strings.Join(Agents(), ", "))
	}
	return filepath.Join(base, rel, name), nil
}

// State says whether the skill in src is installed as name for agent.
func State(base, agent, name, src string) (string, string, error) {
	dst, err := Dir(base, agent, name)
	if err != nil {
		return "", "", err
	}
	info, err := os.Lstat(dst)
	if err != nil {
		return StateNone, dst, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(dst)
		if err == nil && samePath(target, src) {
			return StateLinked, dst, nil
		}
		return StateOther, dst, nil
	}
	if marker, err := os.ReadFile(filepath.Join(dst, SourceMarker)); err == nil {
		if samePath(strings.TrimSpace(string(marker)), src) {
			return StateCopied, dst, nil
		}
	}
	return StateOther, dst, nil
}

func samePath(a, b string) bool {
	ca, err1 := filepath.Abs(a)
	cb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	if ca == cb {
		return true
	}
	ea, err1 := filepath.EvalSymlinks(ca)
	eb, err2 := filepath.EvalSymlinks(cb)
	return err1 == nil && err2 == nil && ea == eb
}

// Link installs the skill folder src as name for agent. With copyFiles (or
// when a symlink cannot be made) the folder is copied. An existing folder
// that maggot did not make is only replaced with force.
func Link(base, agent, name, src string, copyFiles, force bool, copyDir func(src, dst string) error) (string, string, error) {
	state, dst, err := State(base, agent, name, src)
	if err != nil {
		return "", "", err
	}
	if state == StateOther && !force {
		return dst, state, fmt.Errorf("%s already exists and is not this skill; remove it or use --force", dst)
	}
	if state == StateLinked && !copyFiles {
		return dst, StateLinked, nil
	}
	if state != StateNone {
		if err := os.RemoveAll(dst); err != nil {
			return dst, state, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return dst, "", err
	}
	if !copyFiles {
		if err := os.Symlink(src, dst); err == nil {
			return dst, StateLinked, nil
		}
		// No symlinks here (for example Windows without developer mode).
	}
	if err := copyDir(src, dst); err != nil {
		return dst, "", err
	}
	if err := os.WriteFile(filepath.Join(dst, SourceMarker), []byte(src+"\n"), 0o644); err != nil {
		return dst, "", err
	}
	return dst, StateCopied, nil
}

// Unlink removes the skill name for agent when maggot installed it from src.
// It returns whether something was removed.
func Unlink(base, agent, name, src string, force bool) (string, bool, error) {
	state, dst, err := State(base, agent, name, src)
	if err != nil {
		return "", false, err
	}
	switch state {
	case StateNone:
		return dst, false, nil
	case StateOther:
		if !force {
			return dst, false, fmt.Errorf("%s is not this skill; leaving it (use --force to remove it anyway)", dst)
		}
	}
	return dst, true, os.RemoveAll(dst)
}
