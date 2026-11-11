package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/nxnom/maggot/internal/runner"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/workspace"
	"github.com/spf13/cobra"
)

func (a *app) runCmd() *cobra.Command {
	var inputs, outDir, events string
	var skipChecks, dryRun, review bool
	cmd := &cobra.Command{
		Use:   "run <tool> [maggot flags] [-- tool args...]",
		Short: "Run a tool (a thin wrapper; the tool also runs without maggot)",
		Long: `Run a tool from the current directory. Everything after "--" is passed to
the tool unchanged, and the tool's exit code is returned. Flags before "--"
belong to maggot.

Instead of raw args you can pass input values as JSON with --inputs; maggot
turns them into argv using the manifest (the web UI uses the same path).

Before starting, maggot checks that required binaries are on PATH and required
env vars are set. Use 'maggot doctor <tool>' for the full check with versions.`,
		Example: `  maggot run image-resize -- --width 800 photo.png
  maggot run image-resize --inputs '{"width": 800, "files": ["photo.png"]}'
  maggot run --dry-run image-resize -- --width 800 photo.png`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			t, err := e.Set.Resolve(args[0])
			if err != nil {
				return err
			}
			// Plain words before "--" (e.g. "maggot run hello world") also go to the tool.
			toolArgs := args[1:]
			if review {
				if outDir != "" || dryRun {
					return errors.New("--review cannot be used with --out or --dry-run")
				}
				if !skipChecks {
					if err := a.preflight(t); err != nil {
						return err
					}
				}
				var values map[string]any
				if inputs != "" {
					if len(toolArgs) > 0 {
						return errors.New("use either --inputs or raw tool args, not both")
					}
					if values, err = readInputs(inputs); err != nil {
						return err
					}
				}
				return a.runReview(t, toolArgs, values)
			}
			if inputs != "" {
				if len(toolArgs) > 0 {
					return errors.New("use either --inputs or raw tool args, not both")
				}
				if toolArgs, err = argsFromInputs(t, inputs, outDir); err != nil {
					return err
				}
			} else if outDir != "" {
				return errors.New("--out only works with --inputs; pass the tool's own output flag instead")
			}
			if !skipChecks {
				if err := a.preflight(t); err != nil {
					return err
				}
			}
			extra, err := a.secretEnv(t)
			if err != nil {
				return err
			}
			c, err := runner.Command(t.Dir, t.Manifest, toolArgs, runner.Options{ExtraEnv: extra, EventsPath: events})
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Fprintln(a.stdout, strings.Join(quoteAll(c.Args), " "))
				return nil
			}
			c.Stdin, c.Stdout, c.Stderr = a.stdin, a.stdout, a.stderr
			return a.wait(c)
		},
	}
	f := cmd.Flags()
	f.StringVar(&inputs, "inputs", "", "input values as JSON, or @file.json")
	f.StringVar(&outDir, "out", "", "output dir, passed with the manifest's outputs.dir_flag (needs --inputs)")
	f.StringVar(&events, "events", "", "file the tool may append NDJSON events to (sets MAGGOT_EVENTS)")
	f.BoolVar(&skipChecks, "skip-checks", false, "do not check binaries and env before starting")
	f.BoolVar(&dryRun, "dry-run", false, "print the command instead of running it")
	f.BoolVar(&review, "review", false, "hold output files in a run folder for the user to review (see 'maggot take')")
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\n(tool flags go after \"--\": maggot run <tool> -- --flag value)", err)
	})
	return cmd
}

func readInputs(raw string) (map[string]any, error) {
	data := []byte(raw)
	if strings.HasPrefix(raw, "@") {
		b, err := os.ReadFile(raw[1:])
		if err != nil {
			return nil, err
		}
		data = b
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("--inputs must be a JSON object: %w", err)
	}
	return values, nil
}

func argsFromInputs(t *toolbox.Tool, raw, outDir string) ([]string, error) {
	values, err := readInputs(raw)
	if err != nil {
		return nil, err
	}
	return runner.BuildArgs(t.Manifest, values, outDir)
}

// preflight is the cheap version of doctor: binaries on PATH and required env.
func (a *app) preflight(t *toolbox.Tool) error {
	var lookPath func(string) (string, error)
	if a.doctor != nil {
		lookPath = a.doctor.LookPath
	}
	store, _ := a.secretStore()
	if missing := workspace.Missing(t, store, lookPath); len(missing) > 0 {
		return fmt.Errorf("%s cannot run, missing: %s\nrun `maggot doctor %s` for install steps (or --skip-checks)",
			t.Ref(), strings.Join(missing, ", "), t.Ref())
	}
	return nil
}

// wait runs the child and returns its exit code. Ctrl-C reaches the child
// directly (same process group), so maggot ignores it and waits.
func (a *app) wait(c *exec.Cmd) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	err := c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		if code < 0 { // killed by a signal
			code = 130
		}
		return &exitError{code: code}
	}
	return err
}
