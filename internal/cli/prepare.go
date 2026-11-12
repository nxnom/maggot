package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/runner"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/warm"
	"github.com/nxnom/maggot/internal/workspace"
	"github.com/spf13/cobra"
)

// Problem is something that stops a run, with how to fix it.
type Problem struct {
	Kind    string `json:"kind"` // invalid_input, missing_requirement, missing_env, output
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// Decision is a choice that must be made before running (ask the user when
// it is not clear), such as where the files should go.
type Decision struct {
	Kind    string   `json:"kind"` // output_folder
	Message string   `json:"message"`
	Options []string `json:"options,omitempty"`
}

type prepareResult struct {
	Ref string `json:"ref"`
	// Ready is true only when nothing must be fixed (problems) and nothing
	// must be decided (decisions).
	Ready     bool       `json:"ready"`
	Problems  []Problem  `json:"problems"`
	Decisions []Decision `json:"decisions"`
	Warnings  []string   `json:"warnings"`
	Inputs   map[string]any `json:"inputs,omitempty"` // values after defaults
	Argv     []string       `json:"argv,omitempty"`
	Command  string         `json:"command,omitempty"`
	Cwd      string         `json:"cwd"`
	Output   map[string]any `json:"output"`
	Warm     string         `json:"warm,omitempty"` // cold, loading, warm
	Next     string         `json:"next,omitempty"`

	args   []string // tool args the run would use
	outDir string   // real output folder, when known
}

func (a *app) prepareCmd() *cobra.Command {
	var inputs, outDir string
	var review, asJSON bool
	cmd := &cobra.Command{
		Use:   "prepare <tool> [flags] [-- tool args...]",
		Short: "Check a run before doing it: inputs, requirements, output folder, exact command",
		Long: `Check everything about a run without running it: input values (after
defaults), missing binaries or env vars (with how to fix them), where output
goes, whether the tool is warm, the exact command it will run, and the
'maggot run' command to use next. Exit code 1 when something must be fixed.`,
		Example: `  maggot prepare youtube-download --inputs '{"urls":["https://youtu.be/ID"],"mode":"mp3"}' --out ./audio --json
  maggot prepare youtube-download --json -- -x --audio-format mp3 URL`,
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
			var values map[string]any
			if inputs != "" {
				if len(args) > 1 {
					return fmt.Errorf("use either --inputs or raw tool args, not both")
				}
				if values, err = readInputs(inputs); err != nil {
					return err
				}
			}
			res := a.prepare(cmd.Context(), t, values, inputs != "", args[1:], outDir, review)
			if asJSON {
				if err := writeJSON(a.stdout, res); err != nil {
					return err
				}
			} else {
				a.printPrepare(res)
			}
			if !res.Ready {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&inputs, "inputs", "", "input values as JSON, or @file.json")
	f.StringVar(&outDir, "out", "", "output folder (passed with the tool's output flag)")
	f.BoolVar(&review, "review", false, "hold output for review (see 'maggot run --review')")
	f.BoolVar(&asJSON, "json", false, "print JSON")
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\n(tool flags go after \"--\": maggot prepare <tool> -- --flag value)", err)
	})
	return cmd
}

func (a *app) prepare(ctx context.Context, t *toolbox.Tool, values map[string]any, useInputs bool, raw []string, outDir string, review bool) prepareResult {
	m := t.Manifest
	cwd, _ := os.Getwd()
	res := prepareResult{Ref: t.Ref(), Problems: []Problem{}, Decisions: []Decision{}, Warnings: []string{}, Cwd: cwd, Output: map[string]any{}}
	dirFlag := ""
	kind := ""
	if m.Outputs != nil {
		dirFlag, kind = m.Outputs.DirFlag, m.Outputs.Kind
		res.Output["kind"] = kind
	}

	// Output folder.
	if outDir != "" {
		abs, _ := filepath.Abs(outDir)
		outDir = abs
	}
	switch {
	case review && dirFlag == "":
		res.Problems = append(res.Problems, Problem{Kind: "output", Message: "this tool has no output folder flag, so its files cannot be held for review",
			Fix: "run it without --review and pass the destination in the tool's own args"})
	case review:
		res.Output["dir"] = "a run folder held for review (maggot prints its id)"
	case outDir != "" && dirFlag == "":
		res.Problems = append(res.Problems, Problem{Kind: "output", Message: "this tool has no output folder flag (outputs.dir_flag)",
			Fix: "leave out --out; the tool writes where its own args say, or into the current folder"})
	case outDir != "":
		res.Output["dir"] = outDir
		res.outDir = outDir
	case dirFlag != "" && !useInputs && hasFlagValue(raw, dirFlag) != "":
		res.Output["dir"] = hasFlagValue(raw, dirFlag)
		res.outDir, _ = filepath.Abs(hasFlagValue(raw, dirFlag))
	case dirFlag != "":
		res.Output["dir"] = "not chosen"
		res.Decisions = append(res.Decisions, Decision{Kind: "output_folder",
			Message: "choose where the files go; if it is not clear from the task, ask the user",
			Options: []string{"--out <folder> (with --inputs)", dirFlag + " <folder> (in the tool args)", "--review (the user checks them first)"}})
	case kind == manifest.OutputFiles:
		res.Warnings = append(res.Warnings, "this tool has no output folder flag; it writes where its own args say, usually the current folder")
	}

	// Arguments.
	args := raw
	if useInputs {
		flagDir := outDir
		if review {
			flagDir = ""
		}
		built, err := runner.BuildArgs(m, values, flagDir)
		if err != nil {
			for _, line := range strings.Split(err.Error(), "\n") {
				if line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- ")); line != "" && line != "invalid inputs:" {
					res.Problems = append(res.Problems, Problem{Kind: "invalid_input", Message: line, Fix: "see the inputs in `maggot info " + t.Ref() + "`"})
				}
			}
		} else {
			args = built
		}
		res.Inputs = resolvedInputs(m, values)
	} else if outDir != "" && dirFlag != "" && !review {
		args = append([]string{dirFlag, outDir}, raw...)
	}
	res.args = args
	if argv, err := runner.Argv(t.Dir, m, args); err == nil {
		res.Argv = argv
		res.Command = strings.Join(quoteAll(argv), " ")
	}

	// Requirements and env.
	store, _ := a.secretStore()
	checker := a.checker()
	for _, r := range doctor.Requirements(m) {
		if _, err := checker.LookPath(r.Bin); err == nil {
			continue
		}
		p := Problem{Kind: "missing_requirement", Message: r.Bin + " is not installed"}
		if st := checker.Check(ctx, r); st.Install != nil {
			p.Fix = st.Install.String() + "  (ask the user first; or `maggot doctor " + t.Ref() + " --install`)"
		} else if len(st.Alternatives) > 0 {
			for _, c := range st.Alternatives {
				p.Fix = c
				break
			}
		}
		res.Problems = append(res.Problems, p)
	}
	for _, s := range workspace.Env(store, t) {
		if s.Required && !s.Set {
			fix := "export " + s.Name + "=..."
			if s.Secret {
				fix = fmt.Sprintf("ask the user to run: maggot secret set %s %s", t.Ref(), s.Name)
			}
			res.Problems = append(res.Problems, Problem{Kind: "missing_env", Message: s.Name + " is not set", Fix: fix})
		}
	}

	// Warm state.
	if m.Warm != nil {
		res.Warm = "cold"
		if st, _ := warm.Get(m.ID); st != nil {
			res.Warm = "loading"
			if st.Running {
				res.Warm = "warm"
			}
		}
	}

	res.Ready = len(res.Problems) == 0 && len(res.Decisions) == 0
	if len(res.Problems) == 0 {
		// With an open output decision, the command carries a placeholder
		// so it cannot be run as is.
		const choose = "<choose a folder>"
		pending := len(res.Decisions) > 0
		next := []string{"maggot", "run", t.Ref(), "--json"}
		if review {
			next = append(next, "--review")
		}
		if useInputs {
			b, _ := json.Marshal(values)
			next = append(next, "--inputs", string(b))
			switch {
			case outDir != "" && !review:
				next = append(next, "--out", outDir)
			case pending:
				next = append(next, "--out", choose)
			}
		} else {
			if pending || len(args) > 0 {
				next = append(next, "--")
			}
			if pending {
				next = append(next, dirFlag, choose)
			}
			next = append(next, args...)
		}
		res.Next = strings.Join(quoteAll(next), " ")
	}
	return res
}

// resolvedInputs shows every input with the value a run would use.
func resolvedInputs(m *manifest.Manifest, values map[string]any) map[string]any {
	out := map[string]any{}
	for _, in := range m.Inputs {
		if v, ok := values[in.Name]; ok && v != nil {
			out[in.Name] = v
		} else if in.Default != nil {
			out[in.Name] = in.Default
		}
	}
	return out
}

// hasFlagValue returns the value after flag in args ("--out x" or "--out=x").
func hasFlagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(arg, flag+"="); ok {
			return v
		}
	}
	return ""
}

func (a *app) printPrepare(r prepareResult) {
	w := a.stdout
	state := "ready"
	if !r.Ready {
		state = "not ready"
	}
	fmt.Fprintf(w, "%s: %s\n", r.Ref, state)
	for _, p := range r.Problems {
		fmt.Fprintf(w, "  ✗ %s", p.Message)
		if p.Fix != "" {
			fmt.Fprintf(w, "\n      fix: %s", p.Fix)
		}
		fmt.Fprintln(w)
	}
	for _, d := range r.Decisions {
		fmt.Fprintf(w, "  ? %s\n", d.Message)
		for _, o := range d.Options {
			fmt.Fprintf(w, "      - %s\n", o)
		}
	}
	for _, warning := range r.Warnings {
		fmt.Fprintf(w, "  ! %s\n", warning)
	}
	if d, ok := r.Output["dir"]; ok {
		fmt.Fprintf(w, "output:  %v\n", d)
	}
	if r.Warm != "" {
		fmt.Fprintf(w, "warm:    %s\n", r.Warm)
	}
	if r.Command != "" {
		fmt.Fprintf(w, "runs:    %s\n", r.Command)
	}
	if r.Next != "" {
		fmt.Fprintf(w, "next:    %s\n", r.Next)
	}
}
