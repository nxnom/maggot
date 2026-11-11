package cli

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/spf13/cobra"
)

type toolHealth struct {
	Ref          string          `json:"ref"`
	OK           bool            `json:"ok"`
	Requirements []doctor.Result `json:"requirements"`
	Env          []envStatus     `json:"env"`
}

func (a *app) checker() *doctor.Checker {
	if a.doctor != nil {
		return a.doctor
	}
	return doctor.New()
}

func (a *app) health(ctx context.Context, t *toolbox.Tool) toolHealth {
	h := toolHealth{Ref: t.Ref(), OK: true, Requirements: a.checker().CheckAll(ctx, t.Manifest)}
	for _, r := range h.Requirements {
		if !r.OK() {
			h.OK = false
		}
	}
	h.Env = a.envStatus(t)
	for _, e := range h.Env {
		if e.Required && !e.Set {
			h.OK = false
		}
	}
	if h.Requirements == nil {
		h.Requirements = []doctor.Result{}
	}
	if h.Env == nil {
		h.Env = []envStatus{}
	}
	return h
}

func (a *app) doctorCmd() *cobra.Command {
	var asJSON, install, yes bool
	cmd := &cobra.Command{
		Use:   "doctor [tool...]",
		Short: "Check that tools have the binaries and env vars they need",
		Long: `Check required binaries (and their versions) and required env vars.
With no tool names, every tool is checked. Exit code is 1 when something is missing.

--install runs the suggested install commands. It asks before each one;
pass --yes only after the user approved installing software.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			var tools []*toolbox.Tool
			if len(args) == 0 {
				tools, _ = e.set.Tools()
			} else {
				for _, ref := range args {
					t, err := e.set.Resolve(ref)
					if err != nil {
						return err
					}
					tools = append(tools, t)
				}
			}
			ctx := cmd.Context()
			results := make([]toolHealth, 0, len(tools))
			for _, t := range tools {
				results = append(results, a.health(ctx, t))
			}

			if install {
				done := map[string]bool{}
				for _, h := range results {
					for _, r := range h.Requirements {
						if r.OK() || r.Install == nil || done[r.Install.String()] {
							continue
						}
						done[r.Install.String()] = true
						if err := a.confirm(yes, fmt.Sprintf("install %s with `%s`?", r.Bin, r.Install)); err != nil {
							if err == errNotConfirmed {
								fmt.Fprintf(a.stderr, "skipped %s\n", r.Bin)
								continue
							}
							return err
						}
						fmt.Fprintf(a.stderr, "$ %s\n", r.Install)
						if err := a.runInstall(ctx, r.Install.Command); err != nil {
							fmt.Fprintf(a.stderr, "install %s failed: %v\n", r.Bin, err)
						}
					}
				}
				for i, t := range tools { // re-check after installing
					results[i] = a.health(ctx, t)
				}
			}

			allOK := true
			for _, h := range results {
				allOK = allOK && h.OK
			}
			if asJSON {
				if err := writeJSON(a.stdout, map[string]any{"ok": allOK, "tools": results}); err != nil {
					return err
				}
			} else {
				a.printHealth(results)
			}
			if !allOK {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&install, "install", false, "run the suggested install commands (asks first)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask before installing")
	return cmd
}

func (a *app) runInstall(ctx context.Context, argv []string) error {
	if a.installer != nil {
		return a.installer(argv)
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = a.stdin, a.stderr, a.stderr
	return c.Run()
}

func (a *app) printHealth(results []toolHealth) {
	if len(results) == 0 {
		fmt.Fprintln(a.stdout, "no tools to check")
		return
	}
	for _, h := range results {
		mark := "ok"
		if !h.OK {
			mark = "problems"
		}
		fmt.Fprintf(a.stdout, "%s: %s\n", h.Ref, mark)
		for _, r := range h.Requirements {
			switch r.Status {
			case doctor.StatusOK:
				fmt.Fprintf(a.stdout, "  ✓ %s %s\n", r.Bin, r.Version)
			case doctor.StatusMissing:
				fmt.Fprintf(a.stdout, "  ✗ %s not found\n", r.Bin)
			case doctor.StatusOutdated:
				fmt.Fprintf(a.stdout, "  ✗ %s %s does not match %s\n", r.Bin, r.Version, r.Constraint)
			case doctor.StatusUnknown:
				fmt.Fprintf(a.stdout, "  ✗ %s: %s\n", r.Bin, r.Error)
			}
			if !r.OK() {
				if r.Install != nil {
					fmt.Fprintf(a.stdout, "      install: %s\n", r.Install)
				}
				for m, c := range r.Alternatives {
					fmt.Fprintf(a.stdout, "      or (%s): %s\n", m, c)
				}
			}
		}
		for _, e := range h.Env {
			if e.Required && !e.Set {
				hint := "export " + e.Name + "=..."
				if e.Secret {
					hint = fmt.Sprintf("maggot secret set %s %s", h.Ref, e.Name)
				}
				fmt.Fprintf(a.stdout, "  ✗ env %s is not set (%s)\n", e.Name, hint)
			}
		}
	}
}
