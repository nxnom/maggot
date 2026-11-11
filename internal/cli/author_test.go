package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTestImportWorkflow(t *testing.T) {
	isolate(t)
	newToolbox(t, "main")
	work := t.TempDir()

	code, out, e := run(t, "new", "upper-copy", "--lang", "sh", "--dir", work, "--description", "Upper-case copy of text files.")
	if code != 0 || !strings.Contains(out, "maggot test") {
		t.Fatalf("new: %s %s", out, e)
	}
	dir := filepath.Join(work, "upper-copy")

	code, out, _ = run(t, "test", dir)
	if code != 0 || !strings.Contains(out, "3/3 cases passed") {
		t.Fatalf("test: code=%d %s", code, out)
	}

	// Break the tool: a case must fail with a useful report.
	os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho broken >&2\nexit 1\n"), 0o755)
	code, out, _ = run(t, "test", dir, "--json")
	var rep testReport
	if code != 1 || json.Unmarshal([]byte(out), &rep) != nil || rep.OK || rep.Cases[0].Passed || !strings.Contains(rep.Cases[0].Stderr, "broken") {
		t.Fatalf("broken: code=%d %s", code, out)
	}

	// Invalid manifest is reported before running anything.
	os.WriteFile(filepath.Join(dir, "tool.json"), []byte(`{"schema":1}`), 0o644)
	if code, out, _ := run(t, "test", dir); code != 1 || !strings.Contains(out, "✗ manifest") {
		t.Fatalf("manifest: %s", out)
	}
}

func TestTestByRefAndNoTests(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "main")
	addTool(t, tb, "hello", helloManifest, helloScript)
	code, out, _ := run(t, "test", "hello")
	if code != 1 || !strings.Contains(out, "no tests/cases.json") {
		t.Fatalf("no tests: %s", out)
	}
	os.MkdirAll(filepath.Join(tb, "tools", "hello", "tests"), 0o755)
	os.WriteFile(filepath.Join(tb, "tools", "hello", "tests", "cases.json"),
		[]byte(`{"cases":[{"name":"shout","inputs":{"name":"x","shout":true},"expect":{"stdout_contains":["HELLO x"]}}]}`), 0o644)
	if code, out, _ := run(t, "test", "hello"); code != 0 || !strings.Contains(out, "1/1") {
		t.Fatalf("by ref: %s", out)
	}
}

func TestGuide(t *testing.T) {
	code, out, _ := run(t, "guide")
	if code != 0 || !strings.Contains(out, "tests/cases.json") || !strings.Contains(out, "MAGGOT_EVENTS") {
		t.Fatal("guide text missing")
	}
}

func TestNewWarnsAboutSimilarTools(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "main")
	addTool(t, tb, "bg-remover", `{"schema":1,"id":"bg-remover","version":"1.0.0","name":"Background Remover",
	  "description":"Remove the background from photos.","entry":{"path":"run"}}`, "#!/bin/sh\n")
	_, _, e := run(t, "new", "photo-background-cut", "--lang", "sh", "--dir", t.TempDir(), "--description", "Remove photo backgrounds")
	if !strings.Contains(e, "similar tools already exist") || !strings.Contains(e, "main/bg-remover") {
		t.Fatalf("want warning, got %q", e)
	}
	_, _, e = run(t, "new", "pdf-merge", "--lang", "sh", "--dir", t.TempDir(), "--description", "Merge PDF files")
	if strings.Contains(e, "similar") {
		t.Fatalf("unexpected warning: %q", e)
	}
}
