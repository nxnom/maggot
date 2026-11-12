package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/runner"
	"github.com/nxnom/maggot/internal/testlog"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/workspace"
	"github.com/spf13/cobra"
)

type toolSummary struct {
	LastTest    *testlog.Record `json:"last_test"`
	Ref         string          `json:"ref"`
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Categories  []string        `json:"categories"`
	Toolbox     string          `json:"toolbox"`
	Dir         string          `json:"dir"`
}

// summarize is the list/search row for a tool, with its last test result
// (the same record info shows).
func summarize(t *toolbox.Tool, tested map[string]testlog.Record) toolSummary {
	cats := t.Manifest.Categories
	if cats == nil {
		cats = []string{}
	}
	s := toolSummary{
		Ref: t.Ref(), ID: t.Manifest.ID, Version: t.Manifest.Version, Name: t.Manifest.Name,
		Description: t.Manifest.Description, Categories: cats, Toolbox: t.Toolbox.Handle, Dir: t.Dir,
	}
	if rec, ok := tested[t.Ref()]; ok {
		s.LastTest = &rec
	}
	return s
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
			tools, _ := e.Set.Tools()
			tested, _ := testlog.Load()
			rows := []toolSummary{}
			for _, t := range tools {
				if category != "" && !hasCategory(t.Manifest, category) {
					continue
				}
				rows = append(rows, summarize(t, tested))
			}
			problems := e.AllProblems()
			if asJSON {
				if problems == nil {
					problems = []toolbox.Problem{}
				}
				return writeJSON(a.stdout, map[string]any{"tools": rows, "problems": problems})
			}
			a.warnProblems(problems)
			if len(e.Set.Toolboxes) == 0 {
				fmt.Fprintln(a.stdout, "no toolboxes; create one with `maggot toolbox init <path>`")
				return nil
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.stdout, "no tools")
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TOOL\tVERSION\tTESTED\tDESCRIPTION")
			now := time.Now()
			for _, r := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Ref, r.Version, testlog.Label(r.LastTest, r.Version, now), r.Description)
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
	var asJSON, full bool
	cmd := &cobra.Command{
		Use:   "info <tool>",
		Short: "Show what a tool does, its inputs, whether it is ready, and how to run it",
		Long: `Show a short agent-friendly view of a tool: purpose, inputs (with how each
becomes an argument, defaults and choices), readiness, what was tested, and
examples. --full adds the whole manifest and the README.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			t, err := e.Set.Resolve(args[0])
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
			var last *testlog.Record
			if tested, _ := testlog.Load(); tested != nil {
				if rec, ok := tested[t.Ref()]; ok {
					last = &rec
				}
			}
			if asJSON && full {
				return writeJSON(a.stdout, map[string]any{
					"ref": t.Ref(), "toolbox": t.Toolbox.Handle, "dir": t.Dir,
					"direct_command": direct, "manifest": t.Manifest, "readme": readme, "last_test": last,
				})
			}
			if asJSON {
				return writeJSON(a.stdout, a.briefInfo(t, direct, last))
			}
			m := t.Manifest
			fmt.Fprintf(a.stdout, "%s %s — %s\n%s\n\n", t.Ref(), m.Version, m.Name, m.Description)
			fmt.Fprintf(a.stdout, "dir:     %s\n", t.Dir)
			fmt.Fprintf(a.stdout, "tested:  %s\n", testedLine(last, m.Version))
			if last != nil && last.Version == m.Version {
				for _, c := range last.Cases {
					mark := map[string]string{"passed": "✓", "failed": "✗", "skipped": "-"}[c.Status]
					reason := ""
					if c.Reason != "" {
						reason = " (" + c.Reason + ")"
					}
					fmt.Fprintf(a.stdout, "         %s %s%s\n", mark, c.Name, reason)
				}
			}
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
				fmt.Fprintln(a.stdout, "\ninputs (defaults apply with --inputs and the web UI, not to raw args after --):")
				w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
				for _, in := range m.Inputs {
					how := argSummary(in)
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
			if readme != "" && full {
				fmt.Fprintf(a.stdout, "\n%s", readme)
			} else if readme != "" {
				fmt.Fprintf(a.stdout, "\n(README: maggot info %s --full)\n", t.Ref())
			}
			fmt.Fprintf(a.stdout, "\nnext: maggot prepare %s --inputs '{...}' --json\n", t.Ref())
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&full, "full", false, "include the whole manifest and the README")
	return cmd
}

type briefInput struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Accept      []string `json:"accept,omitempty"`
	Arg         string   `json:"arg"`
	Description string   `json:"description,omitempty"`
}

// briefInfo is the compact view agents read before running a tool.
func (a *app) briefInfo(t *toolbox.Tool, direct []string, last *testlog.Record) map[string]any {
	m := t.Manifest
	inputs := []briefInput{}
	for _, in := range m.Inputs {
		bi := briefInput{Name: in.Name, Type: string(in.Type), Required: in.Required, Default: in.Default,
			Min: in.Min, Max: in.Max, Accept: in.Accept, Arg: argSummary(in), Description: in.Description}
		for _, o := range in.Options {
			bi.Choices = append(bi.Choices, o.Value)
		}
		inputs = append(inputs, bi)
	}
	store, _ := a.secretStore()
	missing := workspace.Missing(t, store, nil)
	if missing == nil {
		missing = []string{}
	}
	tested := map[string]any{"status": "never"}
	if last != nil {
		switch {
		case last.Version != m.Version:
			tested = map[string]any{"status": "older version only", "version": last.Version}
		case last.OK:
			tested = map[string]any{"status": "passed", "at": last.At, "cases": last.Cases}
		default:
			tested = map[string]any{"status": "failed", "at": last.At, "error": last.Error, "cases": last.Cases}
		}
	}
	out := map[string]any{
		"ref": t.Ref(), "name": m.Name, "version": m.Version, "description": m.Description,
		"ready": len(missing) == 0, "missing": missing,
		"inputs": inputs, "tested": tested, "examples": m.Examples,
		"direct_command": strings.Join(quoteAll(direct), " "),
		"next":           fmt.Sprintf("maggot prepare %s --inputs '{...}' --json", t.Ref()),
	}
	if m.Outputs != nil {
		out["output"] = m.Outputs
	}
	if len(m.OneOf) > 0 {
		out["one_of"] = m.OneOf // at least one input of each group is needed
	}
	if m.Warm != nil {
		out["warm"] = true
	}
	return out
}

// directCommand is the argv prefix to run the tool without maggot.
func directCommand(t *toolbox.Tool) ([]string, error) {
	return runner.Argv(t.Dir, t.Manifest, nil)
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

func testedLine(r *testlog.Record, version string) string {
	switch {
	case r == nil:
		return "never (run `maggot test <tool>`)"
	case r.Version != version:
		return fmt.Sprintf("only an older version (%s) was tested", r.Version)
	case r.OK:
		return fmt.Sprintf("passed %s ago (%d passed, %d skipped)", testlog.Age(time.Since(r.At)), r.Passed, r.Skipped)
	}
	why := r.Error
	if why == "" {
		why = fmt.Sprintf("%d failed", r.Failed)
	}
	return fmt.Sprintf("FAILED %s ago: %s", testlog.Age(time.Since(r.At)), why)
}

// argSummary describes how an input becomes argv, for `maggot info`.
func argSummary(in manifest.Input) string {
	switch {
	case in.Type == manifest.TypeArgs:
		return "<extra args>"
	case in.ChoiceArgs():
		parts := make([]string, len(in.Options))
		for i, o := range in.Options {
			parts[i] = o.Value + "=" + strings.Join(o.Args, " ")
			if len(o.Args) == 0 {
				parts[i] = o.Value + "=(none)"
			}
		}
		return strings.Join(parts, "; ")
	case len(in.Arg.Args) > 0:
		return strings.Join(in.Arg.Args, " ")
	case in.Arg.Positional:
		return "<positional>"
	case in.Arg.Template != "":
		return in.Arg.Flag + " " + in.Arg.Template
	}
	return in.Arg.Flag
}
