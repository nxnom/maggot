// Package testkit runs a tool's tests/cases.json: each case runs the tool in
// a fresh temp work dir and checks exit code, output text and created files.
package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/runner"
)

// CasesFile is the test file inside a tool folder.
const CasesFile = "tests/cases.json"

type Suite struct {
	Cases []Case `json:"cases"`
}

type Case struct {
	Name string `json:"name"`
	// Args are raw tool args. {tool} is replaced by the tool folder and
	// {work} by the case's temp work dir (also the working directory).
	Args []string `json:"args,omitempty"`
	// Inputs are input values turned into args through the manifest, like
	// the web UI does. Use either Args or Inputs.
	Inputs map[string]any `json:"inputs,omitempty"`
	// Out, with Inputs, is passed as the output dir ({work} allowed).
	Out            string            `json:"out,omitempty"`
	Stdin          string            `json:"stdin,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	// Network marks a case that needs the internet. Skipped unless asked for.
	Network bool `json:"network,omitempty"`
	// Paid marks a case that calls a paid API or uses credits/GPU time.
	// Skipped unless asked for explicitly.
	Paid bool `json:"paid,omitempty"`
	// Retries reruns a failing case up to this many times (max 5).
	Retries int    `json:"retries,omitempty"`
	Expect  Expect `json:"expect"`
}

type Expect struct {
	// Exit is the expected exit code (default 0).
	Exit           int      `json:"exit"`
	StdoutContains []string `json:"stdout_contains,omitempty"`
	StderrContains []string `json:"stderr_contains,omitempty"`
	// Files must exist (relative to {work}) and be non-empty.
	Files []string `json:"files,omitempty"`
	// Checks are commands run after the tool, in {work}, to check the
	// result's content (for example ffprobe on a video).
	Checks []Check `json:"checks,omitempty"`
}

// Check is a command that must exit with Exit (default 0) and print the
// given text on stdout.
type Check struct {
	Run            []string `json:"run"`
	Exit           int      `json:"exit,omitempty"`
	StdoutContains []string `json:"stdout_contains,omitempty"`
}

// Options choose which cases run.
type Options struct {
	Network bool
	Paid    bool
	// SecretEnv (NAME=value) is passed only to network and paid cases, so an
	// offline case can never spend money with a real API key.
	SecretEnv []string
}

type Result struct {
	Name     string   `json:"name"`
	Passed   bool     `json:"passed"`
	Failures []string `json:"failures,omitempty"`
	Command  []string `json:"command"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Duration float64  `json:"duration_seconds"`
	Skipped  string   `json:"skipped,omitempty"` // why the case did not run
	Attempts int      `json:"attempts,omitempty"`
}

// ErrNoTests means the tool has no tests/cases.json.
var ErrNoTests = errors.New("no " + CasesFile)

// Load reads tests/cases.json from a tool folder.
func Load(dir string) (*Suite, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(CasesFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoTests
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Suite
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", CasesFile, err)
	}
	if len(s.Cases) == 0 {
		return nil, fmt.Errorf("%s has no cases", CasesFile)
	}
	for i, c := range s.Cases {
		if c.Name == "" {
			return nil, fmt.Errorf("%s: cases[%d] needs a name", CasesFile, i)
		}
		if c.Args != nil && c.Inputs != nil {
			return nil, fmt.Errorf("%s: case %q: use args or inputs, not both", CasesFile, c.Name)
		}
		if c.Retries < 0 || c.Retries > 5 {
			return nil, fmt.Errorf("%s: case %q: retries must be 0-5", CasesFile, c.Name)
		}
		for j, ch := range c.Expect.Checks {
			if len(ch.Run) == 0 || ch.Run[0] == "" {
				return nil, fmt.Errorf("%s: case %q: checks[%d].run is empty", CasesFile, c.Name, j)
			}
		}
	}
	return &s, nil
}

// Run runs every case allowed by opts; the others are reported as skipped.
func Run(ctx context.Context, dir string, m *manifest.Manifest, s *Suite, opts Options) []Result {
	abs, _ := filepath.Abs(dir)
	results := make([]Result, 0, len(s.Cases))
	for _, c := range s.Cases {
		switch {
		case c.Paid && !opts.Paid:
			results = append(results, Result{Name: c.Name, Skipped: "paid (run with --paid)"})
			continue
		case c.Network && !opts.Network && !c.Paid:
			results = append(results, Result{Name: c.Name, Skipped: "needs network (run with --network)"})
			continue
		}
		var env []string
		if c.Network || c.Paid {
			env = opts.SecretEnv
		}
		var r Result
		for attempt := 1; attempt <= c.Retries+1; attempt++ {
			r = runCase(ctx, abs, m, c, env)
			r.Attempts = attempt
			if r.Passed || ctx.Err() != nil {
				break
			}
		}
		results = append(results, r)
	}
	return results
}

// Summary counts results.
func Summary(results []Result) (passed, failed, skipped int) {
	for _, r := range results {
		switch {
		case r.Skipped != "":
			skipped++
		case r.Passed:
			passed++
		default:
			failed++
		}
	}
	return
}

func runCase(ctx context.Context, dir string, m *manifest.Manifest, c Case, extraEnv []string) Result {
	r := Result{Name: c.Name}
	fail := func(format string, args ...any) { r.Failures = append(r.Failures, fmt.Sprintf(format, args...)) }

	work, err := os.MkdirTemp("", "maggot-test-")
	if err != nil {
		fail("temp dir: %v", err)
		return r
	}
	defer os.RemoveAll(work)
	expand := func(s string) string {
		return strings.NewReplacer("{tool}", dir, "{work}", work).Replace(s)
	}

	var args []string
	if c.Inputs != nil {
		vals := map[string]any{}
		for k, v := range c.Inputs {
			vals[k] = expandValue(v, expand)
		}
		args, err = runner.BuildArgs(m, vals, expand(c.Out))
		if err != nil {
			fail("inputs: %v", err)
			return r
		}
	} else {
		for _, a := range c.Args {
			args = append(args, expand(a))
		}
	}

	env := append([]string{}, extraEnv...)
	for k, v := range c.Env {
		env = append(env, k+"="+expand(v))
	}
	cmd, err := runner.Command(dir, m, args, runner.Options{Dir: work, ExtraEnv: env})
	if err != nil {
		fail("%v", err)
		return r
	}
	r.Command = cmd.Args

	timeout := 60 * time.Second
	if c.TimeoutSeconds > 0 {
		timeout = time.Duration(c.TimeoutSeconds) * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd = exec.CommandContext(cctx, cmd.Path, cmd.Args[1:]...)
	cmd.Dir, cmd.Env = work, append(os.Environ(), envFor(dir, m, env)...)
	cmd.Stdin = strings.NewReader(c.Stdin)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err = cmd.Run()
	r.Duration = time.Since(start).Seconds()
	r.Stdout, r.Stderr = truncate(stdout.String()), truncate(stderr.String())

	code := 0
	var ee *exec.ExitError
	switch {
	case cctx.Err() == context.DeadlineExceeded:
		fail("timed out after %s", timeout)
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case err != nil:
		fail("start: %v", err)
	}
	if err == nil || ee != nil {
		if code != c.Expect.Exit {
			fail("exit code %d, want %d", code, c.Expect.Exit)
		}
	}
	for _, want := range c.Expect.StdoutContains {
		if !strings.Contains(stdout.String(), expand(want)) {
			fail("stdout does not contain %q", want)
		}
	}
	for _, want := range c.Expect.StderrContains {
		if !strings.Contains(stderr.String(), expand(want)) {
			fail("stderr does not contain %q", want)
		}
	}
	for _, f := range c.Expect.Files {
		p := filepath.Join(work, filepath.FromSlash(expand(f)))
		if filepath.IsAbs(expand(f)) {
			p = expand(f)
		}
		info, err := os.Stat(p)
		switch {
		case err != nil:
			fail("file %s was not created", f)
		case info.Size() == 0 && !info.IsDir():
			fail("file %s is empty", f)
		}
	}
	if len(r.Failures) == 0 {
		for _, ch := range c.Expect.Checks {
			if msg := runCheck(ctx, work, ch, expand); msg != "" {
				fail("%s", msg)
			}
		}
	}
	r.Passed = len(r.Failures) == 0
	return r
}

// runCheck runs one check command in the work dir and returns a failure
// message, or "" when it passes.
func runCheck(ctx context.Context, work string, ch Check, expand func(string) string) string {
	args := make([]string, len(ch.Run))
	for i, a := range ch.Run {
		args[i] = expand(a)
	}
	label := "check `" + strings.Join(args, " ") + "`"
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, args[0], args[1:]...)
	cmd.Dir = work
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case err != nil:
		return label + ": " + err.Error()
	}
	if code != ch.Exit {
		return fmt.Sprintf("%s: exit code %d, want %d: %s", label, code, ch.Exit, lastLine(stderr.String()))
	}
	for _, want := range ch.StdoutContains {
		if !strings.Contains(stdout.String(), expand(want)) {
			return fmt.Sprintf("%s: stdout does not contain %q (got %q)", label, want, truncateTo(stdout.String(), 200))
		}
	}
	return ""
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return truncateTo(s, 200)
}

func truncateTo(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// envFor mirrors runner.Command's env so the rebuilt command behaves the same.
func envFor(dir string, m *manifest.Manifest, extra []string) []string {
	return append([]string{
		runner.EnvToolDir + "=" + dir,
		runner.EnvToolID + "=" + m.ID,
		runner.EnvToolVersion + "=" + m.Version,
	}, extra...)
}

func expandValue(v any, expand func(string) string) any {
	switch x := v.(type) {
	case string:
		return expand(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = expandValue(item, expand)
		}
		return out
	}
	return v
}

func truncate(s string) string {
	const max = 4000
	if len(s) > max {
		return s[:max] + "\n…(truncated)"
	}
	return s
}
