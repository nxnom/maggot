package toolbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/manifest"
)

// ArchiveDir is the folder inside a toolbox that holds archived tools and
// skills. It starts with a dot, so the loaders skip it.
const ArchiveDir = ".archive"

const (
	KindTool  = "tool"
	KindSkill = "skill"
)

// Archived is a tool or skill moved to a toolbox's archive.
type Archived struct {
	Kind        string `json:"kind"` // KindTool or KindSkill
	Name        string `json:"name"`
	Toolbox     string `json:"toolbox"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
}

// Ref is "<toolbox>/<name>".
func (a Archived) Ref() string { return a.Toolbox + "/" + a.Name }

func (tb *Toolbox) archivePath(kind, name string) string {
	return filepath.Join(tb.Path, ArchiveDir, kind+"s", name)
}

func (tb *Toolbox) livePath(kind, name string) string {
	if kind == KindSkill {
		return filepath.Join(tb.SkillsPath(), name)
	}
	return filepath.Join(tb.ToolsPath(), name)
}

// Archive moves a live tool or skill folder into the archive.
func (tb *Toolbox) Archive(kind, name string) error {
	dst := tb.archivePath(kind, name)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s is already in the archive; restore or delete it first", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(tb.livePath(kind, name), dst)
}

// Restore moves an archived tool or skill back to its live folder. It never
// overwrites an existing folder.
func (tb *Toolbox) Restore(kind, name string) error {
	dst := tb.livePath(kind, name)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists; not overwriting", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(tb.archivePath(kind, name), dst)
}

// Archived lists the archived tools and skills, sorted by reference.
func (tb *Toolbox) Archived() []Archived {
	var out []Archived
	for _, kind := range []string{KindTool, KindSkill} {
		dir := filepath.Join(tb.Path, ArchiveDir, kind+"s")
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			it := Archived{Kind: kind, Name: e.Name(), Toolbox: tb.Handle, Path: filepath.Join(dir, e.Name())}
			if kind == KindTool {
				if m, err := manifest.Load(it.Path); err == nil {
					it.Description = m.Description
				}
			} else if sk, err := agentskill.LoadNamed(it.Path); err == nil {
				it.Description = sk.Meta.Description
			}
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
	return out
}

// Archived lists archived items from all toolboxes.
func (s *Set) Archived() []Archived {
	var out []Archived
	for _, tb := range s.Toolboxes {
		out = append(out, tb.Archived()...)
	}
	return out
}
