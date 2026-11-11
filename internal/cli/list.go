package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/spf13/cobra"
)

type toolSummary struct {
	Ref         string   `json:"ref"`
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Categories  []string `json:"categories"`
	Toolbox     string   `json:"toolbox"`
	Dir         string   `json:"dir"`
}

func summarize(t *toolbox.Tool) toolSummary {
	cats := t.Manifest.Categories
	if cats == nil {
		cats = []string{}
	}
	return toolSummary{
		Ref: t.Ref(), ID: t.Manifest.ID, Version: t.Manifest.Version, Name: t.Manifest.Name,
		Description: t.Manifest.Description, Categories: cats, Toolbox: t.Toolbox.Handle, Dir: t.Dir,
	}
}

func (a *app) listCmd() *cobra.Command {
	var asJSON bool
	var category string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tools in all toolboxes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			tools, _ := e.set.Tools()
			rows := []toolSummary{}
			for _, t := range tools {
				if category != "" && !hasCategory(t.Manifest, category) {
					continue
				}
				rows = append(rows, summarize(t))
			}
			problems := e.allProblems()
			if asJSON {
				if problems == nil {
					problems = []toolbox.Problem{}
				}
				return writeJSON(a.stdout, map[string]any{"tools": rows, "problems": problems})
			}
			a.warnProblems(problems)
			if len(e.set.Toolboxes) == 0 {
				fmt.Fprintln(a.stdout, "no toolboxes; create one with `maggot toolbox init <path>`")
				return nil
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.stdout, "no tools")
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TOOL\tVERSION\tDESCRIPTION")
			for _, r := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\n", r.Ref, r.Version, r.Description)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&category, "category", "", "only tools in this category")
	return cmd
}

func hasCategory(m *manifest.Manifest, c string) bool {
	for _, x := range m.Categories {
		if strings.EqualFold(x, c) {
			return true
		}
	}
	return false
}

func (a *app) infoCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "info <tool>",
		Short: "Show a tool's manifest, README and how to run it directly",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			t, err := e.set.Resolve(args[0])
			if err != nil {
				return err
			}
			direct, err := directCommand(t)
			if err != nil {
				return err
			}
			readme := ""
			if b, err := os.ReadFile(filepath.Join(t.Dir, "README.md")); err == nil {
				readme = string(b)
			}
			if asJSON {
				return writeJSON(a.stdout, map[string]any{
					"ref": t.Ref(), "toolbox": t.Toolbox.Handle, "dir": t.Dir,
					"direct_command": direct, "manifest": t.Manifest, "readme": readme,
				})
			}
			m := t.Manifest
			fmt.Fprintf(a.stdout, "%s %s — %s\n%s\n\n", t.Ref(), m.Version, m.Name, m.Description)
			fmt.Fprintf(a.stdout, "dir:     %s\n", t.Dir)
			fmt.Fprintf(a.stdout, "run:     maggot run %s -- <args>\n", t.Ref())
			fmt.Fprintf(a.stdout, "direct:  %s <args>\n", strings.Join(quoteAll(direct), " "))
			if len(m.Requires) > 0 {
				fmt.Fprintln(a.stdout, "\nrequires:")
				for _, r := range m.Requires {
					fmt.Fprintf(a.stdout, "  %s %s\n", r.Bin, r.Version)
				}
			}
			if len(m.Env) > 0 {
				fmt.Fprintln(a.stdout, "\nenv:")
				for _, v := range m.Env {
					flags := []string{}
					if v.Secret {
						flags = append(flags, "secret")
					}
					if v.Required {
						flags = append(flags, "required")
					}
					fmt.Fprintf(a.stdout, "  %s %v %s\n", v.Name, flags, v.Description)
				}
			}
			if len(m.Inputs) > 0 {
				fmt.Fprintln(a.stdout, "\ninputs:")
				w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
				for _, in := range m.Inputs {
					how := in.Arg.Flag
					if in.Arg.Positional {
						how = "<positional>"
					}
					req := ""
					if in.Required {
						req = "required"
					}
					fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", in.Name, in.Type, how, req, in.Description)
				}
				w.Flush()
			}
			if len(m.Examples) > 0 {
				fmt.Fprintln(a.stdout, "\nexamples:")
				for _, ex := range m.Examples {
					fmt.Fprintf(a.stdout, "  %s\n", ex)
				}
			}
			if readme != "" {
				fmt.Fprintf(a.stdout, "\n%s", readme)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// directCommand is the argv prefix to run the tool without maggot.
func directCommand(t *toolbox.Tool) ([]string, error) {
	entry, err := t.Manifest.EntryPath(t.Dir)
	if err != nil {
		return nil, err
	}
	return append(append([]string{}, t.Manifest.Entry.Interpreter...), entry), nil
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, s := range args {
		if s == "" || strings.ContainsAny(s, " \t\"'$&|;<>()*?[]#~") {
			s = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
		out[i] = s
	}
	return out
}
