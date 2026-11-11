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
