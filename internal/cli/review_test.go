package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const makerManifest = `{"schema":1,"id":"maker","version":"1.0.0","name":"Maker","description":"Makes files.",
  "entry":{"path":"run"},
  "inputs":[{"name":"name","type":"string","required":true,"arg":{"positional":true}}],
  "outputs":{"kind":"files","dir_flag":"--out"}}`

const makerScript = `#!/bin/sh
[ "$1" = "--out" ] && { out="$2"; shift 2; }
echo "making $1"
echo "content $1" > "$out/$1.txt"
`

var runIDRe = regexp.MustCompile(`review run (\S+):`)

func TestReviewFlow(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "main")
	addTool(t, tb, "maker", makerManifest, makerScript)
	addTool(t, tb, "hello", helloManifest, helloScript)

	code, out, e := run(t, "run", "maker", "--review", "--", "logo")
	if code != 0 || out != "making logo\n" || !strings.Contains(e, "maggot take") || !strings.Contains(e, "logo.txt") {
		t.Fatalf("review run: code=%d out=%q err=%s", code, out, e)
	}
	id := runIDRe.FindStringSubmatch(e)[1]

	_, out, _ = run(t, "runs", "--json")
	var listed struct {
		Runs []struct {
			ID     string
			Review bool
			Files  []struct{ Path string }
		}
	}
	json.Unmarshal([]byte(out), &listed)
	if len(listed.Runs) != 1 || listed.Runs[0].ID != id || !listed.Runs[0].Review || len(listed.Runs[0].Files) != 1 {
		t.Fatalf("runs: %s", out)
	}

	dest := filepath.Join(t.TempDir(), "assets")
	if code, out, e := run(t, "take", id, dest); code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "logo.txt") {
		t.Fatalf("take: %s %s", out, e)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "logo.txt")); string(b) != "content logo\n" {
		t.Fatal("not moved")
	}
	if _, out, _ := run(t, "runs"); !strings.Contains(out, "no runs") {
		t.Fatalf("run should be gone: %s", out)
	}

	// Reject: runs rm. Inputs work too.
	_, _, e = run(t, "run", "maker", "--review", "--inputs", `{"name":"draft"}`)
	id = runIDRe.FindStringSubmatch(e)[1]
	if code, _, e := run(t, "runs", "rm", id); code != 0 {
		t.Fatal(e)
	}
	if code, _, e := run(t, "take", id, dest); code == 0 {
		t.Fatalf("take after rm should fail: %s", e)
	}

	// A tool without an output folder flag cannot be reviewed.
	if code, _, e := run(t, "run", "hello", "--review", "--", "x"); code == 0 || !strings.Contains(e, "outputs.dir_flag") {
		t.Fatalf("hello: %s", e)
	}
}
