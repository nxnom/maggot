package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftsListAndResolve(t *testing.T) {
	isolate(t)
	newToolbox(t, "main")
	drafts := filepath.Join(os.Getenv("MAGGOT_DATA_DIR"), "drafts")

	if code, out, _ := run(t, "drafts"); code != 0 || !strings.Contains(out, "no drafts") {
		t.Fatalf("empty: %d %q", code, out)
	}

	if code, _, e := run(t, "new", "fresh-tool", "--lang", "sh"); code != 0 {
		t.Fatal(e)
	}
	if code, out, e := run(t, "skill", "new", "my-flow", "--description", "Does a flow. Use when testing."); code != 0 {
		t.Fatalf("skill new: %s %s", out, e)
	}
	script := filepath.Join(drafts, "skills", "my-flow", "scripts", "hi.sh")
	os.MkdirAll(filepath.Dir(script), 0o755)
	os.WriteFile(script, []byte("echo hi\n"), 0o644)

	code, out, e := run(t, "drafts", "--json")
	var rows []draftRow
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 2 {
		t.Fatalf("json: %d %s %s", code, out, e)
	}
	if rows[0].Kind != "skill" || rows[0].Ref != "draft/my-flow" || rows[1].Kind != "tool" || rows[1].Ref != "draft/fresh-tool" {
		t.Fatalf("rows: %+v", rows)
	}
	if _, out, _ := run(t, "drafts"); !strings.Contains(out, "my-flow") || !strings.Contains(out, "fresh-tool") {
		t.Fatalf("text: %s", out)
	}

	// A skill draft works by name and as draft/<name> with show, run and path.
	for _, name := range []string{"my-flow", "draft/my-flow"} {
		if code, out, e := run(t, "skill", "run", name, "hi.sh"); code != 0 || out != "hi\n" {
			t.Fatalf("run %s: %d %q %s", name, code, out, e)
		}
		if code, out, _ := run(t, "skill", "path", name); code != 0 || strings.TrimSpace(out) != filepath.Join(drafts, "skills", "my-flow") {
			t.Fatalf("path %s: %d %q", name, code, out)
		}
	}
	if code, _, e := run(t, "skill", "show", "draft/nope"); code == 0 || !strings.Contains(e, "maggot drafts") {
		t.Fatalf("missing draft: %s", e)
	}
	// A tool draft works by name with test.
	if code, out, e := run(t, "test", "draft/fresh-tool"); code != 0 {
		t.Fatalf("test draft: %s %s", out, e)
	}

	// Once installed, the toolbox copy wins by plain name and the list shows the installed version.
	if code, _, e := run(t, "import", "--yes", "--keep-draft", "fresh-tool"); code != 0 {
		t.Fatalf("import by name: %s", e)
	}
	_, out, _ = run(t, "drafts", "--json")
	json.Unmarshal([]byte(out), &rows)
	for _, r := range rows {
		if r.Name == "fresh-tool" && r.Installed == "" {
			t.Fatalf("installed version missing: %+v", r)
		}
	}
}
