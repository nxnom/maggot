package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nxnom/maggot/internal/secrets"
	"github.com/nxnom/maggot/internal/toolbox"
)

// SkillSecretKey is the secret store key for a skill. Skills and tools have
// separate names, so a skill and a tool with the same name do not share
// secrets.
func SkillSecretKey(name string) string { return "skill:" + name }

// ToolNeed is a maggot tool a skill uses.
type ToolNeed struct {
	Name string `json:"name"` // as written in the skill
	Ref  string `json:"ref,omitempty"`
	// Found is false when no toolbox has the tool.
	Found bool `json:"found"`
	// Missing lists what the tool itself still needs (binaries, env).
	Missing []string `json:"missing,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// BinNeed is a program a skill needs on PATH.
type BinNeed struct {
	Bin   string `json:"bin"`
	Path  string `json:"path,omitempty"`
	Found bool   `json:"found"`
}

// SkillCheck says whether everything a skill needs is there.
type SkillCheck struct {
	Ready    bool        `json:"ready"`
	Missing  []string    `json:"missing"`
	Tools    []ToolNeed  `json:"tools"`
	Requires []BinNeed   `json:"requires"`
	Env      []EnvStatus `json:"env"`
}

// SkillEnv reports each env var a skill declares. Every one counts as a
// required secret: a value in the environment wins over a stored one.
func SkillEnv(store secrets.Store, sk *toolbox.Skill) []EnvStatus {
	out := []EnvStatus{}
	for _, name := range sk.Env {
		s := EnvStatus{Name: name, Secret: true, Required: true}
		if _, ok := os.LookupEnv(name); ok {
			s.Set, s.Source = true, "env"
		} else if store != nil {
			if _, ok, _ := store.Get(SkillSecretKey(sk.Meta.Name), name); ok {
				s.Set, s.Source = true, "store"
			}
		}
		out = append(out, s)
	}
	return out
}

// SkillSecretEnv returns NAME=value pairs for stored secrets of a skill
// that are not already in the environment.
func SkillSecretEnv(store secrets.Store, sk *toolbox.Skill) ([]string, error) {
	var out []string
	if store == nil {
		return nil, nil
	}
	for _, name := range sk.Env {
		if _, ok := os.LookupEnv(name); ok {
			continue
		}
		val, ok, err := store.Get(SkillSecretKey(sk.Meta.Name), name)
		if err != nil {
			return nil, fmt.Errorf("read secret %s: %w", name, err)
		}
		if ok {
			out = append(out, name+"="+val)
		}
	}
	return out, nil
}

// CheckSkill checks the tools, programs and secrets a skill declares.
// lookPath may be nil (exec.LookPath).
func CheckSkill(set *toolbox.Set, sk *toolbox.Skill, store secrets.Store, lookPath func(string) (string, error)) SkillCheck {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	c := SkillCheck{Missing: []string{}, Tools: []ToolNeed{}, Requires: []BinNeed{}}
	for _, name := range sk.Uses {
		n := ToolNeed{Name: name}
		t, err := set.Resolve(name)
		if err != nil {
			n.Error = err.Error()
			c.Missing = append(c.Missing, "tool "+name)
		} else {
			n.Found, n.Ref = true, t.Ref()
			n.Missing = Missing(t, store, lookPath)
			if len(n.Missing) > 0 {
				c.Missing = append(c.Missing, "tool "+name+" (needs "+strings.Join(n.Missing, ", ")+")")
			}
		}
		c.Tools = append(c.Tools, n)
	}
	for _, bin := range sk.Requires {
		b := BinNeed{Bin: bin}
		if p, err := lookPath(bin); err == nil {
			b.Found, b.Path = true, p
		} else {
			c.Missing = append(c.Missing, "binary "+bin)
		}
		c.Requires = append(c.Requires, b)
	}
	c.Env = SkillEnv(store, sk)
	for _, e := range c.Env {
		if !e.Set {
			c.Missing = append(c.Missing, "env "+e.Name)
		}
	}
	c.Ready = len(c.Missing) == 0
	return c
}
