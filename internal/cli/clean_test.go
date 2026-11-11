package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportDeletesDraft(t *testing.T) {
	isolate(t)
	newToolbox(t, "main")
	drafts := filepath.Join(os.Getenv("MAGGOT_DATA_DIR"), "drafts")
	run(t, "new", "one", "--lang", "sh")
	run(t, "new", "two", "--lang", "sh")
	if _, out, _ := run(t, "import", "--yes", filepath.Join(drafts, "one")); !strings.Contains(out, "deleted the draft") {
		t.Fatalf("import: %s", out)
	}
	if _, err := os.Stat(filepath.Join(drafts, "one")); err == nil {
		t.Fatal("draft should be gone")
	}
	run(t, "import", "--yes", "--keep-draft", filepath.Join(drafts, "two"))
	if _, err := os.Stat(filepath.Join(drafts, "two")); err != nil {
		t.Fatal("--keep-draft should keep it")
	}
	// A folder outside the drafts folder is never deleted.
	other := t.TempDir()
	run(t, "new", "three", "--lang", "sh", "--dir", other)
	run(t, "import", "--yes", filepath.Join(other, "three"))
	if _, err := os.Stat(filepath.Join(other, "three")); err != nil {
		t.Fatal("non-draft folder must stay")
	}
}

func TestClean(t *testing.T) {
	isolate(t)
	data := os.Getenv("MAGGOT_DATA_DIR")
	mk := func(rel, runJSON string, age time.Duration) {
		p := filepath.Join(data, rel)
		os.MkdirAll(p, 0o755)
		if runJSON != "" {
			os.WriteFile(filepath.Join(p, "run.json"), []byte(runJSON), 0o644)
		}
		os.WriteFile(filepath.Join(p, "f.bin"), make([]byte, 2048), 0o644)
		old := time.Now().Add(-age)
		os.Chtimes(p, old, old)
	}
	mk("runs/20260101-000000-aaaaaa", `{"tool_id":"yt","status":"succeeded"}`, 10*24*time.Hour)
	mk("runs/20260102-000000-bbbbbb", `{"tool_id":"pdf","status":"failed"}`, time.Hour)
	mk("runs/20260103-000000-cccccc", `{"tool_id":"yt","status":"running","started":"`+time.Now().Format(time.RFC3339)+`"}`, 0)
	mk("uploads/x", "", time.Hour)
	run(t, "new", "drafty", "--lang", "sh")

	code, out, _ := run(t, "clean")
	if code != 0 || !strings.Contains(out, "runs        2") || !strings.Contains(out, "drafts: drafty") {
		t.Fatalf("report: %s", out)
	}
	if code, _, e := run(t, "clean", "runs"); code == 0 || !strings.Contains(e, "--yes") {
		t.Fatalf("should ask: %s", e)
	}
	_, out, _ = run(t, "clean", "runs", "--older-than", "7d", "--dry-run")
	if !strings.Contains(out, "aaaaaa") || strings.Contains(out, "bbbbbb") {
		t.Fatalf("dry run: %s", out)
	}
	run(t, "clean", "runs", "--tool", "pdf", "--yes")
	if _, err := os.Stat(filepath.Join(data, "runs/20260102-000000-bbbbbb")); err == nil {
		t.Fatal("pdf run should be gone")
	}
	run(t, "clean", "all", "--yes")
	if _, err := os.Stat(filepath.Join(data, "runs/20260103-000000-cccccc")); err != nil {
		t.Fatal("running run must be kept")
	}
	for _, p := range []string{"runs/20260101-000000-aaaaaa", "uploads/x", "drafts/drafty"} {
		if _, err := os.Stat(filepath.Join(data, p)); err == nil {
			t.Errorf("%s should be deleted", p)
		}
	}
	if code, _, e := run(t, "clean", "stuff"); code == 0 || !strings.Contains(e, "unknown kind") {
		t.Fatalf("bad kind: %s", e)
	}
}
