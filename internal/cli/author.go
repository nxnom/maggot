package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/guide"
	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/nxnom/maggot/internal/scaffold"
	"github.com/nxnom/maggot/internal/search"
	"github.com/nxnom/maggot/internal/testkit"
	"github.com/nxnom/maggot/internal/testlog"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/spf13/cobra"
)

func (a *app) draftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "draft <tool>",
		Short: "Copy an installed tool into the drafts folder to change it",
		Long: `Copy an installed tool into the drafts folder. Edit and test the copy,
let the user try it with 'maggot try', bump its version, then import it to
replace the installed tool.`,
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
			drafts, err := config.DraftsDir()
			if err != nil {
				return err
			}
			dst := filepath.Join(drafts, t.Manifest.ID)
			if err := pack.CopyTool(t.Dir, dst); err != nil {
				return fmt.Errorf("%w (delete or finish the existing draft first)", err)
			}
			fmt.Fprintf(a.stdout, "%s\nedit it, bump \"version\" in tool.json, `maggot test %s`, then `maggot try %s`\n", dst, dst, dst)
			return nil
		},
	}
}

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
	var warmMode bool
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
		Example: `  maggot new media-download --lang bin --bin yt-dlp --dir /tmp/work
  maggot new image-resize --lang python --dir /tmp/work`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				var err error
				if dir, err = config.DraftsDir(); err != nil {
					return err
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			path, err := scaffold.Create(dir, scaffold.Options{ID: args[0], Lang: lang, Name: name, Description: desc, Bin: bin, Warm: warmMode})
			if err != nil {
				return err
			}
			a.warnSimilar(args[0], desc)
			abs, _ := filepath.Abs(path)
			fmt.Fprintf(a.stdout, "created %s\nnext: edit it, `maggot test %s`, then let the user try it with `maggot try %s`\n", abs, abs, abs)
			return nil
		},
	}
	cmd.Flags().StringVar(&lang, "lang", "", "kind of tool: "+strings.Join(scaffold.Langs(), ", ")+" (required)")
	cmd.Flags().StringVar(&bin, "bin", "", "program to run, for --lang bin")
	cmd.Flags().BoolVar(&warmMode, "warm", false, "python: add a serve mode that keeps a slow-loading model in memory (see `maggot warm`)")
	_ = cmd.MarkFlagRequired("lang")
	cmd.Flags().StringVar(&dir, "dir", "", "parent folder for the new tool (default: the maggot drafts folder)")
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

// toolDirArg accepts a tool folder path or a tool reference. ref is the
// installed tool's reference, or "" for a folder outside the toolboxes.
func (a *app) toolDirArg(arg string) (dir, ref string, err error) {
	e, envErr := a.loadEnv()
	if _, statErr := os.Stat(filepath.Join(arg, manifest.FileName)); statErr == nil {
		dir, err = filepath.Abs(arg)
		if err != nil || envErr != nil {
			return dir, "", err
		}
		tools, _ := e.Set.Tools()
		for _, t := range tools {
			if t.Dir == dir {
				return dir, t.Ref(), nil
			}
		}
		return dir, "", nil
	}
	if strings.HasPrefix(arg, draftHandle+"/") {
		if p, ok := draftToolDir(arg); ok {
			return p, "", nil
		}
		return "", "", fmt.Errorf("no tool draft %q (see `maggot drafts`)", arg)
	}
	if envErr != nil {
		return "", "", envErr
	}
	t, err := e.Set.Resolve(arg)
	if err != nil {
		if p, ok := draftToolDir(arg); ok {
			fmt.Fprintf(a.stderr, "note: using the draft of %s (not imported yet)\n", arg)
			return p, "", nil
		}
		return "", "", fmt.Errorf("%s is not a tool folder and %w", arg, err)
	}
	return t.Dir, t.Ref(), nil
}

func (a *app) testCmd() *cobra.Command {
	var asJSON, verbose, all bool
	var opts testOpts
	cmd := &cobra.Command{
		Use:   "test <tool-dir|tool> | --all",
		Short: "Validate a tool and run its tests/cases.json",
		Long: `Validate the manifest, check requirements, then run tests/cases.json.

Cases marked "network": true run only with --network. Cases marked
"paid": true (paid APIs, credits, GPU time) run only with --paid. Stored
secrets are passed only to those cases, so offline cases cannot spend money.

Results of installed tools are saved (outside the toolbox) and shown by
'maggot list' and 'maggot info'. Run 'maggot test --all' now and then to find
tools that stopped working, for example after a site or program changed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) > 0 {
					return errors.New("use either --all or a tool, not both")
				}
				return a.testAll(cmd.Context(), opts, asJSON)
			}
			if len(args) == 0 {
				return errors.New("name a tool or folder, or use --all")
			}
			dir, ref, err := a.toolDirArg(args[0])
			if err != nil {
				return err
			}
			rep := a.testTool(cmd.Context(), dir, opts)
			a.recordTest(ref, dir, rep)
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
	cmd.Flags().BoolVar(&all, "all", false, "test every tool in the toolboxes")
	opts.flags(cmd)
	return cmd
}

// recordTest saves the result of a tool. An installed tool is saved under its
// reference; a draft or other folder is saved under "draft/<id>" and moves
// to the installed tool when it is imported unchanged. Tools without tests
// are not recorded, so they show as never tested.
func (a *app) recordTest(ref, dir string, rep testReport) {
	if rep.manifest == nil || rep.noTests {
		return
	}
	if ref == "" {
		ref = testlog.DraftKey(rep.manifest.ID)
	}
	errMsg := rep.ManifestErr
	if errMsg == "" {
		errMsg = rep.TestsErr
	}
	var cases []testlog.Case
	for _, c := range rep.Cases {
		tc := testlog.Case{Name: c.Name, Status: "passed"}
		switch {
		case c.Skipped != "":
			tc.Status, tc.Reason = "skipped", c.Skipped
		case !c.Passed:
			tc.Status = "failed"
			if len(c.Failures) > 0 {
				tc.Reason = firstLine(c.Failures[0])
			}
		}
		cases = append(cases, tc)
	}
	_ = testlog.Save(ref, testlog.Record{Version: rep.manifest.Version, OK: rep.OK,
		Passed: rep.Passed, Failed: rep.Failed, Skipped: rep.Skipped, Error: firstLine(errMsg), Cases: cases,
		Hash: testlog.DirHash(dir)})
}

func (a *app) testAll(ctx context.Context, opts testOpts, asJSON bool) error {
	e, err := a.loadEnv()
	if err != nil {
		return err
	}
	tools, _ := e.Set.Tools()
	type row struct {
		Ref    string     `json:"ref"`
		Report testReport `json:"report"`
	}
	rows := []row{}
	failed, untested := 0, 0
	for _, t := range tools {
		rep := a.testTool(ctx, t.Dir, opts)
		a.recordTest(t.Ref(), t.Dir, rep)
		rows = append(rows, row{t.Ref(), rep})
		switch {
		case rep.noTests:
			untested++
		case !rep.OK:
			failed++
		}
		if asJSON {
			continue
		}
		switch {
		case rep.noTests:
			fmt.Fprintf(a.stdout, "- %s: no tests\n", t.Ref())
		case rep.OK:
			skipped := ""
			if rep.Skipped > 0 {
				skipped = fmt.Sprintf(", %d skipped", rep.Skipped)
			}
			fmt.Fprintf(a.stdout, "✓ %s: %d passed%s\n", t.Ref(), rep.Passed, skipped)
		default:
			why := rep.ManifestErr + rep.TestsErr
			if why == "" {
				why = fmt.Sprintf("%d failed (run `maggot test %s` for details)", rep.Failed, t.Ref())
			}
			fmt.Fprintf(a.stdout, "✗ %s: %s\n", t.Ref(), firstLine(why))
		}
	}
	if asJSON {
		if err := writeJSON(a.stdout, map[string]any{"ok": failed == 0, "failed": failed, "untested": untested, "tools": rows}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(a.stdout, "%d tools: %d ok, %d failed, %d without tests\n", len(tools), len(tools)-failed-untested, failed, untested)
	}
	if failed > 0 {
		return &exitError{code: 1}
	}
	return nil
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
	noTests  bool
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
			rep.noTests = true
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
