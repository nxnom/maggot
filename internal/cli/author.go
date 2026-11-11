package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/guide"
	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/scaffold"
	"github.com/nxnom/maggot/internal/testkit"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/spf13/cobra"
)

func (a *app) guideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print the tool authoring guide (manifest, run contract, tests)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(a.stdout, guide.Text)
			return err
		},
	}
}

func (a *app) newCmd() *cobra.Command {
	var lang, dir, name, desc string
	cmd := &cobra.Command{
		Use:   "new <id>",
		Short: "Create a new tool folder from a working template",
		Long: `Create <dir>/<id> with tool.json, an entry program, README.md and tests.
The template works and passes 'maggot test'; change it into your tool, then
'maggot import' it. Run 'maggot guide' for the full manifest reference.`,
		Example: "  maggot new image-resize --lang python --dir /tmp/work",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := scaffold.Create(dir, scaffold.Options{ID: args[0], Lang: lang, Name: name, Description: desc})
			if err != nil {
				return err
			}
			abs, _ := filepath.Abs(path)
			fmt.Fprintf(a.stdout, "created %s\nnext: edit it, then `maggot test %s` and `maggot import %s`\n", abs, abs, abs)
			return nil
		},
	}
	cmd.Flags().StringVar(&lang, "lang", "python", "template: "+strings.Join(scaffold.Langs(), ", "))
	cmd.Flags().StringVar(&dir, "dir", ".", "parent folder for the new tool")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&desc, "description", "", "one-line description")
	return cmd
}

// toolDirArg accepts a tool folder path or a tool reference.
func (a *app) toolDirArg(arg string) (string, error) {
	if _, err := os.Stat(filepath.Join(arg, manifest.FileName)); err == nil {
		return filepath.Abs(arg)
	}
	e, err := a.loadEnv()
	if err != nil {
		return "", err
	}
	t, err := e.set.Resolve(arg)
	if err != nil {
		return "", fmt.Errorf("%s is not a tool folder and %w", arg, err)
	}
	return t.Dir, nil
}

func (a *app) testCmd() *cobra.Command {
	var asJSON, verbose bool
	cmd := &cobra.Command{
		Use:   "test <tool-dir|tool>",
		Short: "Validate a tool and run its tests/cases.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := a.toolDirArg(args[0])
			if err != nil {
				return err
			}
			rep := a.testTool(cmd.Context(), dir)
			if asJSON {
				if err := writeJSON(a.stdout, rep); err != nil {
					return err
				}
			} else {
				a.printTestReport(rep, verbose)
			}
			if !rep.OK {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show output of passing cases too")
	return cmd
}

type testReport struct {
	Dir          string           `json:"dir"`
	OK           bool             `json:"ok"`
	ManifestErr  string           `json:"manifest_error,omitempty"`
	Requirements []doctor.Result  `json:"requirements"`
	TestsErr     string           `json:"tests_error,omitempty"`
	Cases        []testkit.Result `json:"cases"`
}

func (a *app) testTool(ctx context.Context, dir string) testReport {
	rep := testReport{Dir: dir, Requirements: []doctor.Result{}, Cases: []testkit.Result{}}
	m, err := manifest.Load(dir)
	if err != nil {
		rep.ManifestErr = err.Error()
		return rep
	}
	if base := filepath.Base(dir); base != m.ID {
		rep.ManifestErr = fmt.Sprintf("folder name %q must match tool id %q", base, m.ID)
		return rep
	}
	rep.Requirements = a.checker().CheckAll(ctx, m)
	for _, r := range rep.Requirements {
		if !r.OK() {
			rep.TestsErr = "requirements missing; run `maggot doctor` steps first"
			return rep
		}
	}
	suite, err := testkit.Load(dir)
	if err != nil {
		rep.TestsErr = err.Error()
		if errors.Is(err, testkit.ErrNoTests) {
			rep.TestsErr += " (every tool should have tests; see `maggot guide`)"
		}
		return rep
	}
	t := &toolbox.Tool{Dir: dir, Manifest: m, Toolbox: &toolbox.Toolbox{}}
	env, err := a.secretEnv(t)
	if err != nil {
		rep.TestsErr = err.Error()
		return rep
	}
	rep.Cases = testkit.Run(ctx, dir, m, suite, env)
	rep.OK = true
	for _, c := range rep.Cases {
		rep.OK = rep.OK && c.Passed
	}
	return rep
}

func (a *app) printTestReport(rep testReport, verbose bool) {
	w := a.stdout
	if rep.ManifestErr != "" {
		fmt.Fprintf(w, "✗ manifest\n%s\n", indent(rep.ManifestErr))
		return
	}
	fmt.Fprintln(w, "✓ manifest")
	for _, r := range rep.Requirements {
		if r.OK() {
			fmt.Fprintf(w, "✓ requires %s %s\n", r.Bin, r.Version)
		} else {
			fmt.Fprintf(w, "✗ requires %s: %s\n", r.Bin, r.Status)
		}
	}
	if rep.TestsErr != "" {
		fmt.Fprintf(w, "✗ tests: %s\n", rep.TestsErr)
		return
	}
	passed := 0
	for _, c := range rep.Cases {
		if c.Passed {
			passed++
			fmt.Fprintf(w, "✓ %s (%.2fs)\n", c.Name, c.Duration)
		} else {
			fmt.Fprintf(w, "✗ %s\n", c.Name)
			for _, f := range c.Failures {
				fmt.Fprintf(w, "    - %s\n", f)
			}
		}
		if !c.Passed || verbose {
			fmt.Fprintf(w, "    $ %s\n", strings.Join(quoteAll(c.Command), " "))
			if c.Stdout != "" {
				fmt.Fprintf(w, "    stdout:\n%s\n", indent(strings.TrimRight(c.Stdout, "\n")))
			}
			if c.Stderr != "" {
				fmt.Fprintf(w, "    stderr:\n%s\n", indent(strings.TrimRight(c.Stderr, "\n")))
			}
		}
	}
	fmt.Fprintf(w, "%d/%d cases passed\n", passed, len(rep.Cases))
}

func indent(s string) string {
	return "      " + strings.ReplaceAll(s, "\n", "\n      ")
}
