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
 "outputs":{"dir_flag":"--out"}}`

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
	res := Run(context.Background(), dir, m, s, nil)
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
