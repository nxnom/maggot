package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/spf13/cobra"
)

const draftHandle = "draft"

// draftRow is one folder in the drafts folder.
type draftRow struct {
	Kind        string `json:"kind"` // tool or skill
	Ref         string `json:"ref"`  // draft/<name>
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Installed   string `json:"installed,omitempty"` // version of the same name in a toolbox
	Error       string `json:"error,omitempty"`
}

// listDrafts reads the tool and skill drafts. Installed versions are filled in
// when e is not nil.
func listDrafts(e *env) ([]draftRow, error) {
	var rows []draftRow
	dir, err := config.DraftsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, en := range entries {
		p := filepath.Join(dir, en.Name())
		if !en.IsDir() || strings.HasPrefix(en.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(p, manifest.FileName)); err != nil {
			continue
		}
		r := draftRow{Kind: "tool", Name: en.Name(), Path: p}
		if m, err := manifest.Load(p); err != nil {
			r.Error = err.Error()
		} else {
			r.Name, r.Version, r.Description = m.ID, m.Version, m.Description
			if e != nil {
				if t, err := e.Set.Resolve(m.ID); err == nil {
					r.Installed = t.Manifest.Version
				}
			}
		}
		r.Ref = draftHandle + "/" + r.Name
		rows = append(rows, r)
	}
	sdir, err := config.SkillDraftsDir()
	if err != nil {
		return nil, err
	}
	entries, _ = os.ReadDir(sdir)
	for _, en := range entries {
		p := filepath.Join(sdir, en.Name())
		if !en.IsDir() || strings.HasPrefix(en.Name(), ".") || !agentskill.Exists(p) {
			continue
		}
		r := draftRow{Kind: "skill", Name: en.Name(), Path: p}
		if sk, err := agentskill.LoadNamed(p); err != nil {
			r.Error = err.Error()
		} else {
			r.Name, r.Version, r.Description = sk.Meta.Name, sk.Version, sk.Meta.Description
			if e != nil {
				if s, err := e.Set.ResolveSkill(sk.Meta.Name); err == nil {
					r.Installed = s.Version
				}
			}
		}
		r.Ref = draftHandle + "/" + r.Name
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

// draftSkill loads the skill draft called name ("name" or "draft/name").
func draftSkill(name string) (*toolbox.Skill, bool) {
	name = strings.TrimPrefix(name, draftHandle+"/")
	dir, err := config.SkillDraftsDir()
	if err != nil || name == "" || strings.ContainsAny(name, `/\`) {
		return nil, false
	}
	p := filepath.Join(dir, name)
	if !agentskill.Exists(p) {
		return nil, false
	}
	sk, err := agentskill.LoadNamed(p)
	if err != nil {
		return nil, false
	}
	return &toolbox.Skill{Skill: sk, Toolbox: &toolbox.Toolbox{Handle: draftHandle, Path: dir}}, true
}

// draftToolDir returns the folder of the tool draft called name ("name" or "draft/name").
func draftToolDir(name string) (string, bool) {
	name = strings.TrimPrefix(name, draftHandle+"/")
	dir, err := config.DraftsDir()
	if err != nil || name == "" || strings.ContainsAny(name, `/\`) || name == "skills" {
		return "", false
	}
	p := filepath.Join(dir, name)
	if _, err := os.Stat(filepath.Join(p, manifest.FileName)); err != nil {
		return "", false
	}
	return p, true
}

func (a *app) draftsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "drafts",
		Short: "List tool and skill drafts (made with 'maggot new' or 'maggot skill new')",
		Long: `List what is in the drafts folder: tools and skills an agent made that the
user has not imported yet. Drafts are not in 'maggot list' or 'maggot search'.

Use a draft by its name, or as draft/<name> when a toolbox has the same name:
  maggot test draft/<tool>      maggot try <tool>     maggot import <tool>
  maggot skill show|run|path draft/<skill> ...`,
		Example: `  maggot drafts
  maggot drafts --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, _ := a.loadEnv()
			rows, err := listDrafts(e)
			if err != nil {
				return err
			}
			if asJSON {
				if rows == nil {
					rows = []draftRow{}
				}
				return writeJSON(a.stdout, rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.stdout, "no drafts")
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "KIND\tNAME\tVERSION\tINSTALLED\tPATH")
			for _, r := range rows {
				inst := "-"
				if r.Installed != "" {
					inst = r.Installed
				}
				ver := r.Version
				if r.Error != "" {
					ver = "broken: " + oneLine(r.Error, 60)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Kind, r.Name, ver, inst, r.Path)
			}
			return w.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

// draftPath turns a draft name ("name" or "draft/name") into its folder when
// arg is not an existing path. A tool draft wins over a skill draft with the
// same name. Other arguments come back unchanged.
func draftPath(arg string) string {
	if _, err := os.Stat(arg); err == nil {
		return arg
	}
	if p, ok := draftToolDir(arg); ok {
		return p
	}
	if sk, ok := draftSkill(arg); ok {
		return sk.Dir
	}
	return arg
}
