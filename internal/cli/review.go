package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/ui"
	"github.com/nxnom/maggot/internal/warm"
	"github.com/spf13/cobra"
)

func runManager() (*ui.Manager, error) {
	data, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return ui.NewManager(filepath.Join(data, "runs"), cfg.Keep()), nil
}

// runReview runs a tool into a run folder the user can look at in the web
// UI, then prints how to move the files into place.
func (a *app) runReview(t *toolbox.Tool, args []string, values map[string]any) error {
	_, err := a.reviewRun(t, args, values, false)
	return err
}

// reviewRun runs a tool into a held run folder. With quiet, the tool's
// output goes to stderr and the closing hints are not printed (the caller
// prints a JSON result instead).
func (a *app) reviewRun(t *toolbox.Tool, args []string, values map[string]any, quiet bool) (*ui.RunMeta, error) {
	m := t.Manifest
	if m.Outputs == nil || m.Outputs.DirFlag == "" {
		return nil, fmt.Errorf("%s has no output folder flag (outputs.dir_flag), so its files cannot be held for review; run it without --review", t.Ref())
	}
	mgr, err := runManager()
	if err != nil {
		return nil, err
	}
	env, err := a.secretEnv(t)
	if err != nil {
		return nil, err
	}
	env = append(env, warm.Env(t)...)
	a.waitForWarm(t)
	cwd, _ := os.Getwd()
	opts := ui.StartOptions{ExtraEnv: env, Cwd: cwd, Review: true, Origin: "cli"}
	if values != nil {
		opts.Values = values
	} else {
		opts.Args = append([]string{}, args...)
	}
	run, err := mgr.StartWith(t, opts)
	if err != nil {
		return nil, err
	}
	hist, ch, stop := run.Subscribe()
	defer stop()
	print := func(msg ui.Message) {
		switch msg.Type {
		case "out":
			if quiet {
				fmt.Fprintln(a.stderr, msg.Text)
			} else {
				fmt.Fprintln(a.stdout, msg.Text)
			}
		case "err":
			fmt.Fprintln(a.stderr, msg.Text)
		}
	}
	for _, msg := range hist {
		print(msg)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
loop:
	for {
		select {
		case msg := <-ch:
			print(msg)
		case <-sig:
			mgr.Cancel(run.Meta().ID)
		case <-run.Done():
			for len(ch) > 0 {
				print(<-ch)
			}
			break loop
		}
	}
	meta := run.Meta()
	if quiet {
		return &meta, nil
	}
	files := mgr.Outputs(meta)
	fmt.Fprintf(a.stderr, "\nreview run %s: %s, %d file(s) in %s\n", meta.ID, meta.Status, len(files), meta.OutDir)
	for _, f := range files {
		fmt.Fprintf(a.stderr, "  %s\n", f.Path)
	}
	if len(files) > 0 && meta.Status == ui.StatusSucceeded {
		link := "maggot ui"
		if base, token, ok := ui.Running(); ok {
			link = ui.ToolURL(base, token, t.Ref()) + "?run=" + meta.ID
		}
		fmt.Fprintf(a.stderr, "let the user look at them: %s\n", link)
		fmt.Fprintf(a.stderr, "if approved, move them into place: maggot take %s <folder>\n", meta.ID)
		fmt.Fprintf(a.stderr, "if rejected: maggot runs rm %s\n", meta.ID)
	}
	if meta.ExitCode != nil && *meta.ExitCode != 0 {
		return &meta, &exitError{code: *meta.ExitCode}
	}
	if meta.Status != ui.StatusSucceeded {
		return &meta, &exitError{code: 1}
	}
	return &meta, nil
}

func (a *app) takeCmd() *cobra.Command {
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "take <run-id> <folder> [file...]",
		Short: "Move a run's output files into a folder and delete the run",
		Long: `Move the output files of a run (for example a 'maggot run --review' the user
approved) into a folder, keeping sub folders, then delete the run. Files are
moved, not copied, so no second copy is left behind. Name files to take only
some of them; the rest are deleted with the run.`,
		Example: `  maggot take 20261112-101500-a1b2c3 ./assets/icons
  maggot take 20261112-101500-a1b2c3 ./assets icon-512.png icon-1024.png`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := runManager()
			if err != nil {
				return err
			}
			moved, err := mgr.Take(args[0], args[1], args[2:], overwrite)
			if err != nil {
				return err
			}
			for _, p := range moved {
				fmt.Fprintln(a.stdout, p)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace files that already exist in the folder")
	return cmd
}

func (a *app) runsCmd() *cobra.Command {
	var asJSON bool
	var tool string
	var limit int
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List saved runs (web UI runs and review runs)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := runManager()
			if err != nil {
				return err
			}
			list := mgr.List(tool, limit)
			type row struct {
				ui.RunMeta
				Files []ui.OutputFile `json:"files"`
			}
			rows := []row{}
			for _, r := range list {
				rows = append(rows, row{r, mgr.Outputs(r)})
			}
			if asJSON {
				return writeJSON(a.stdout, map[string]any{"runs": rows})
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.stdout, "no runs")
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "RUN\tTOOL\tSTATUS\tFILES\tSTARTED")
			for _, r := range rows {
				status := string(r.Status)
				if r.Review {
					status += " (review)"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", r.ID, r.ToolID, status, len(r.Files), r.Started.Format(time.DateTime))
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&tool, "tool", "", "only runs of this tool id")
	cmd.Flags().IntVar(&limit, "limit", 20, "max runs (0 = all)")
	rm := &cobra.Command{
		Use:   "rm <run-id>...",
		Short: "Delete runs and their output files",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := runManager()
			if err != nil {
				return err
			}
			var failed []string
			for _, id := range args {
				if meta, err := mgr.Get(id); err == nil && meta.Status == ui.StatusRunning {
					failed = append(failed, id+": still running")
					continue
				}
				if err := mgr.Delete(id); err != nil {
					failed = append(failed, id+": "+err.Error())
				}
			}
			if len(failed) > 0 {
				return errors.New(strings.Join(failed, "; "))
			}
			return nil
		},
	}
	cmd.AddCommand(rm)
	return cmd
}
