package testkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/manifest"
)

const tm = `{"schema":1,"id":"t","version":"1.0.0","name":"T","description":"d","entry":{"path":"run"},
 "inputs":[{"name":"name","type":"string","arg":{"positional":true}}],
 "outputs":{"kind":"files","dir_flag":"--out"}}`

// script: prints greeting, writes out/<name>.txt when --out is given, exit 2 for "bad".
const script = `#!/bin/sh
out=""
if [ "$1" = "--out" ]; then out="$2"; shift 2; fi
[ "$1" = "bad" ] && { echo "bad input" >&2; exit 2; }
[ "$1" = "slow" ] && sleep 5
echo "hi $1 $GREETING"
if [ -n "$out" ]; then mkdir -p "$out"; echo x > "$out/$1.txt"; fi
`

func setup(t *testing.T, cases string) (string, *manifest.Manifest) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tool.json"), []byte(tm), 0o644)
	os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755)
	if cases != "" {
		os.MkdirAll(filepath.Join(dir, "tests"), 0o755)
		os.WriteFile(filepath.Join(dir, "tests", "cases.json"), []byte(cases), 0o644)
	}
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, m
}

func TestRunCases(t *testing.T) {
	dir, m := setup(t, `{"cases":[
	  {"name":"args", "args":["bob"], "env":{"GREETING":"yo"}, "expect":{"stdout_contains":["hi bob yo"]}},
	  {"name":"inputs and files", "inputs":{"name":"ann"}, "out":"{work}/out", "expect":{"files":["out/ann.txt"]}},
	  {"name":"expected failure", "args":["bad"], "expect":{"exit":2, "stderr_contains":["bad input"]}},
	  {"name":"wrong expectation", "args":["x"], "expect":{"stdout_contains":["nope"], "files":["missing.txt"]}},
	  {"name":"timeout", "args":["slow"], "timeout_seconds":1, "expect":{}}
	]}`)
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(context.Background(), dir, m, s, Options{})
	pass := map[string]bool{}
	for _, r := range res {
		pass[r.Name] = r.Passed
	}
	if !pass["args"] || !pass["inputs and files"] || !pass["expected failure"] {
		t.Fatalf("should pass: %+v", res)
	}
	if pass["wrong expectation"] || len(res[3].Failures) != 2 {
		t.Fatalf("wrong expectation: %+v", res[3])
	}
	if pass["timeout"] || !strings.Contains(strings.Join(res[4].Failures, ""), "timed out") {
		t.Fatalf("timeout: %+v", res[4])
	}
}

func TestLoadErrors(t *testing.T) {
	dir, _ := setup(t, "")
	if _, err := Load(dir); !errors.Is(err, ErrNoTests) {
		t.Fatalf("want ErrNoTests, got %v", err)
	}
	for body, want := range map[string]string{
		`{"cases":[]}`:            "no cases",
		`{"cases":[{"args":[]}]}`: "needs a name",
		`{"cases":[{"name":"x","args":[],"inputs":{}}]}`:   "not both",
		`{"cases":[{"name":"x","expect":{"exitcode":1}}]}`: "unknown field",
	} {
		dir, _ := setup(t, body)
		if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", body, want, err)
		}
	}
}

func TestSkipRetryChecksAndSecrets(t *testing.T) {
	dir, m := setup(t, `{"cases":[
	  {"name":"offline", "args":["a"], "expect":{"stdout_contains":["hi a "]}},
	  {"name":"online", "network":true, "args":["b"], "expect":{"stdout_contains":["hi b secret"]}},
	  {"name":"costly", "paid":true, "args":["c"], "expect":{}},
	  {"name":"content", "inputs":{"name":"d"}, "out":"{work}/o", "expect":{"checks":[
	    {"run":["cat","o/d.txt"], "stdout_contains":["x"]},
	    {"run":["sh","-c","exit 3"], "exit":3}
	  ]}},
	  {"name":"bad content", "inputs":{"name":"e"}, "out":"{work}/o", "expect":{"checks":[{"run":["grep","-q","nope","o/e.txt"]}]}},
	  {"name":"flaky", "retries":2, "args":["x"], "expect":{"files":["{work}/never"]}}
	]}`)
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{SecretEnv: []string{"GREETING=secret"}}
	res := Run(context.Background(), dir, m, s, opts)
	by := map[string]Result{}
	for _, r := range res {
		by[r.Name] = r
	}
	// The offline case must not see the secret.
	if !by["offline"].Passed || strings.Contains(by["offline"].Stdout, "secret") {
		t.Fatalf("offline: %+v", by["offline"])
	}
	if by["online"].Skipped == "" || by["costly"].Skipped == "" {
		t.Fatalf("network/paid should be skipped by default: %+v %+v", by["online"], by["costly"])
	}
	if !by["content"].Passed {
		t.Fatalf("content: %+v", by["content"])
	}
	if by["bad content"].Passed || !strings.Contains(strings.Join(by["bad content"].Failures, ""), "check `grep") {
		t.Fatalf("bad content: %+v", by["bad content"])
	}
	if by["flaky"].Passed || by["flaky"].Attempts != 3 {
		t.Fatalf("retries: %+v", by["flaky"])
	}
	if p, f, sk := Summary(res); p != 2 || f != 2 || sk != 2 {
		t.Fatalf("summary %d %d %d", p, f, sk)
	}

	// With --network the online case runs and gets the secret; paid stays skipped.
	opts.Network = true
	res = Run(context.Background(), dir, m, s, opts)
	if !res[1].Passed || res[2].Skipped == "" {
		t.Fatalf("network run: %+v %+v", res[1], res[2])
	}
	opts.Paid = true
	if res = Run(context.Background(), dir, m, s, opts); res[2].Skipped != "" || !res[2].Passed {
		t.Fatalf("paid run: %+v", res[2])
	}
}

func TestLoadRejectsBadChecks(t *testing.T) {
	for body, want := range map[string]string{
		`{"cases":[{"name":"x","retries":9,"expect":{}}]}`:          "retries",
		`{"cases":[{"name":"x","expect":{"checks":[{"run":[]}]}}]}`: "run is empty",
	} {
		dir, _ := setup(t, body)
		if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", body, want, err)
		}
	}
}
