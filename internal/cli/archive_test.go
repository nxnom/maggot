package cli

import (
	"strings"
	"testing"
)

func TestArchiveRestore(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "box")
	addTool(t, tb, "hello", helloManifest, helloScript)

	if code, _, errOut := run(t, "archive", "hello"); code != 0 {
		t.Fatalf("archive: %s", errOut)
	}
	_, out, _ := run(t, "search", "greeting")
	if strings.Contains(out, "hello") {
		t.Fatalf("archived tool still found: %s", out)
	}
	if code, _, _ := run(t, "run", "hello", "x"); code == 0 {
		t.Fatal("archived tool still runs")
	}
	_, out, _ = run(t, "archived")
	if !strings.Contains(out, "tool\tbox/hello") {
		t.Fatalf("archived list: %q", out)
	}
	if code, _, errOut := run(t, "restore", "hello"); code != 0 {
		t.Fatalf("restore: %s", errOut)
	}
	if code, out, _ := run(t, "run", "hello", "x"); code != 0 || !strings.Contains(out, "hello x") {
		t.Fatalf("restored tool: %d %q", code, out)
	}
}

func TestBrief(t *testing.T) {
	cases := map[string]string{
		"Make maps. Then more text.":                     "Make maps.",
		"Remove a background (e.g. AI art). Keeps hair.": "Remove a background (e.g. AI art).",
		"No full stop here":                              "No full stop here",
	}
	for in, want := range cases {
		if got := brief(in, 80); got != want {
			t.Errorf("brief(%q) = %q, want %q", in, got, want)
		}
	}
	if got := brief(strings.Repeat("a", 100), 10); len([]rune(got)) != 10 {
		t.Errorf("not cut: %q", got)
	}
}
