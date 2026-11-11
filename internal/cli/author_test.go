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

func TestTestAllRecordsResults(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "main")
	work := t.TempDir()
	run(t, "new", "good-tool", "--lang", "sh", "--dir", work)
	run(t, "import", "--yes", filepath.Join(work, "good-tool"))
	addTool(t, tb, "hello", helloManifest, helloScript) // no tests
	run(t, "new", "bad-tool", "--lang", "sh", "--dir", work)
	os.WriteFile(filepath.Join(work, "bad-tool", "run.sh"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	run(t, "import", "--yes", filepath.Join(work, "bad-tool"))

	if _, out, _ := run(t, "list"); !strings.Contains(out, "TESTED") || !strings.Contains(out, "main/good-tool  0.1.0    -") {
		t.Fatalf("untested list: %s", out)
	}
	code, out, _ := run(t, "test", "--all")
	if code != 1 || !strings.Contains(out, "✓ main/good-tool: 3 passed") || !strings.Contains(out, "✗ main/bad-tool") ||
		!strings.Contains(out, "- main/hello: no tests") || !strings.Contains(out, "3 tools: 1 ok, 1 failed, 1 without tests") {
		t.Fatalf("test --all: code=%d\n%s", code, out)
	}
	_, out, _ = run(t, "list")
	if !strings.Contains(out, "ok 0m") || !strings.Contains(out, "FAIL 0m") {
		t.Fatalf("list after test: %s", out)
	}
	if _, out, _ := run(t, "info", "bad-tool"); !strings.Contains(out, "tested:  FAILED") {
		t.Fatalf("info: %s", out)
	}
	if _, out, _ := run(t, "info", "hello"); !strings.Contains(out, "tested:  never") {
		t.Fatalf("info never: %s", out)
	}
	// A scratch folder outside the toolboxes is not recorded.
	run(t, "new", "scratch-tool", "--lang", "sh", "--dir", work)
	run(t, "test", filepath.Join(work, "scratch-tool"))
	var listed struct{ Tools []toolSummary }
	_, out, _ = run(t, "list", "--json")
	json.Unmarshal([]byte(out), &listed)
	for _, tl := range listed.Tools {
		if tl.ID == "good-tool" && (tl.LastTest == nil || !tl.LastTest.OK) {
			t.Fatalf("json last_test: %+v", tl)
		}
	}
	if code, _, e := run(t, "test"); code == 0 || !strings.Contains(e, "--all") {
		t.Fatalf("no args: %s", e)
	}
}

func TestNewDefaultsToDraftsAndDraftCommand(t *testing.T) {
	isolate(t)
	newToolbox(t, "main")
	drafts := filepath.Join(os.Getenv("MAGGOT_DATA_DIR"), "drafts")
	code, out, e := run(t, "new", "fresh-tool", "--lang", "sh")
	if code != 0 || !strings.Contains(out, filepath.Join(drafts, "fresh-tool")) || !strings.Contains(out, "maggot try") {
		t.Fatalf("new: %s %s", out, e)
	}
	run(t, "import", "--yes", filepath.Join(drafts, "fresh-tool"))
	os.RemoveAll(filepath.Join(drafts, "fresh-tool"))
	code, out, _ = run(t, "draft", "fresh-tool")
	if code != 0 || !strings.HasPrefix(out, filepath.Join(drafts, "fresh-tool")) {
		t.Fatalf("draft: %s", out)
	}
	if _, err := os.Stat(filepath.Join(drafts, "fresh-tool", "tests", "cases.json")); err != nil {
		t.Fatal("draft should include tests")
	}
	if code, _, e := run(t, "draft", "fresh-tool"); code == 0 || !strings.Contains(e, "already exists") {
		t.Fatalf("second draft: %s", e)
	}
}
