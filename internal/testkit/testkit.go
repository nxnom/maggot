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
	Expect         Expect            `json:"expect"`
}

type Expect struct {
	// Exit is the expected exit code (default 0).
	Exit           int      `json:"exit"`
	StdoutContains []string `json:"stdout_contains,omitempty"`
	StderrContains []string `json:"stderr_contains,omitempty"`
	// Files must exist (relative to {work}) and be non-empty.
	Files []string `json:"files,omitempty"`
}

type Result struct {
	Name     string   `json:"name"`
	Passed   bool     `json:"passed"`
	Failures []string `json:"failures,omitempty"`
	Command  []string `json:"command"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Duration float64  `json:"duration_seconds"`
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
	}
	return &s, nil
}

// Run runs every case. extraEnv is added to each case (for example secrets).
func Run(ctx context.Context, dir string, m *manifest.Manifest, s *Suite, extraEnv []string) []Result {
	abs, _ := filepath.Abs(dir)
	results := make([]Result, 0, len(s.Cases))
	for _, c := range s.Cases {
		results = append(results, runCase(ctx, abs, m, c, extraEnv))
	}
	return results
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
	r.Passed = len(r.Failures) == 0
	return r
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
