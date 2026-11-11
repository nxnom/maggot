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
	"github.com/nxnom/maggot/internal/search"
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
	var lang, dir, name, desc, bin string
	cmd := &cobra.Command{
		Use:   "new <id>",
		Short: "Create a new tool folder from a working template",
		Long: `Create <dir>/<id> with tool.json, README.md, tests and (except for bin) an
entry program. The template works and passes 'maggot test'; change it into
your tool, then 'maggot import' it. Run 'maggot guide' for the full reference.

Pick the simplest kind that works:
  bin     a program on PATH already does the job (yt-dlp, ffmpeg, magick):
          no code, only a manifest with inputs mapped to its flags
  sh      small glue around existing programs
  python  real logic or libraries (uv installs inline dependencies)
  deno    real logic or libraries in TypeScript (npm:/jsr: imports)`,
		Example: `  maggot new youtube-download --lang bin --bin yt-dlp --dir /tmp/work
  maggot new image-resize --lang python --dir /tmp/work`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := scaffold.Create(dir, scaffold.Options{ID: args[0], Lang: lang, Name: name, Description: desc, Bin: bin})
			if err != nil {
				return err
			}
			a.warnSimilar(args[0], desc)
			abs, _ := filepath.Abs(path)
			fmt.Fprintf(a.stdout, "created %s\nnext: edit it, then `maggot test %s` and `maggot import %s`\n", abs, abs, abs)
			return nil
		},
	}
	cmd.Flags().StringVar(&lang, "lang", "", "kind of tool: "+strings.Join(scaffold.Langs(), ", ")+" (required)")
	cmd.Flags().StringVar(&bin, "bin", "", "program to run, for --lang bin")
	_ = cmd.MarkFlagRequired("lang")
	cmd.Flags().StringVar(&dir, "dir", ".", "parent folder for the new tool")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&desc, "description", "", "one-line description")
	return cmd
}

// warnSimilar lists existing tools that look like the new one, so agents
// improve a tool instead of adding a near-duplicate.
func (a *app) warnSimilar(id, desc string) {
	e, err := a.loadEnv()
	if err != nil {
		return
	}
	tools, _ := e.Set.Tools()
	hits := search.Rank(tools, strings.ReplaceAll(id, "-", " ")+" "+desc)
	var similar []string
	for _, h := range hits {
		// Only strong matches: most query words matched.
		if len(h.Matched) >= 2 || h.Tool.Manifest.ID == id {
			similar = append(similar, fmt.Sprintf("  %s — %s", h.Tool.Ref(), h.Tool.Manifest.Description))
		}
		if len(similar) == 5 {
			break
		}
	}
	if len(similar) > 0 {
		fmt.Fprintf(a.stderr, "note: similar tools already exist; if one almost fits, improve it (bump its version) instead:\n%s\n",
			strings.Join(similar, "\n"))
	}
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
	t, err := e.Set.Resolve(arg)
	if err != nil {
		return "", fmt.Errorf("%s is not a tool folder and %w", arg, err)
	}
	return t.Dir, nil
}

func (a *app) testCmd() *cobra.Command {
	var asJSON, verbose bool
	var opts testOpts
	cmd := &cobra.Command{
		Use:   "test <tool-dir|tool>",
		Short: "Validate a tool and run its tests/cases.json",
		Long: `Validate the manifest, check requirements, then run tests/cases.json.

Cases marked "network": true run only with --network. Cases marked
"paid": true (paid APIs, credits, GPU time) run only with --paid. Stored
secrets are passed only to those cases, so offline cases cannot spend money.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := a.toolDirArg(args[0])
			if err != nil {
				return err
			}
			rep := a.testTool(cmd.Context(), dir, opts)
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
	opts.flags(cmd)
	return cmd
}

type testOpts struct{ network, paid bool }

func (o *testOpts) flags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&o.network, "network", false, "also run cases that need the internet")
	cmd.Flags().BoolVar(&o.paid, "paid", false, "also run cases that call paid APIs (may cost money)")
}

type testReport struct {
	Dir          string           `json:"dir"`
	OK           bool             `json:"ok"`
	ManifestErr  string           `json:"manifest_error,omitempty"`
	Requirements []doctor.Result  `json:"requirements"`
	TestsErr     string           `json:"tests_error,omitempty"`
	Cases        []testkit.Result `json:"cases"`
	Passed       int              `json:"passed"`
	Failed       int              `json:"failed"`
	Skipped      int              `json:"skipped"`

	manifest *manifest.Manifest
}

func (a *app) testTool(ctx context.Context, dir string, opts testOpts) testReport {
	rep := testReport{Dir: dir, Requirements: []doctor.Result{}, Cases: []testkit.Result{}}
	m, err := manifest.Load(dir)
	if err != nil {
		rep.ManifestErr = err.Error()
		return rep
	}
	rep.manifest = m
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
	run := testkit.Options{Network: opts.network, Paid: opts.paid}
	if opts.network || opts.paid {
		t := &toolbox.Tool{Dir: dir, Manifest: m, Toolbox: &toolbox.Toolbox{}}
		if run.SecretEnv, err = a.secretEnv(t); err != nil {
			rep.TestsErr = err.Error()
			return rep
		}
	}
	rep.Cases = testkit.Run(ctx, dir, m, suite, run)
	rep.Passed, rep.Failed, rep.Skipped = testkit.Summary(rep.Cases)
	rep.OK = rep.Failed == 0
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
	for _, c := range rep.Cases {
		switch {
		case c.Skipped != "":
			fmt.Fprintf(w, "- %s (skipped: %s)\n", c.Name, c.Skipped)
			continue
		case c.Passed:
			tries := ""
			if c.Attempts > 1 {
				tries = fmt.Sprintf(", attempt %d", c.Attempts)
			}
			fmt.Fprintf(w, "✓ %s (%.2fs%s)\n", c.Name, c.Duration, tries)
		default:
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
	line := fmt.Sprintf("%d/%d cases passed", rep.Passed, rep.Passed+rep.Failed)
	if rep.Skipped > 0 {
		line += fmt.Sprintf(", %d skipped", rep.Skipped)
	}
	fmt.Fprintln(w, line)
}

func indent(s string) string {
	return "      " + strings.ReplaceAll(s, "\n", "\n      ")
}
