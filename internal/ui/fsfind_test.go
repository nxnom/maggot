package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccepts(t *testing.T) {
	cases := []struct {
		name   string
		accept []string
		want   bool
	}{
		{"lines.JSON", []string{".json", ".txt"}, true},
		{"notes.txt", []string{".json", ".txt"}, true},
		{"voice.wav", []string{".json", ".txt"}, false},
		{"voice.wav", []string{"audio/*"}, true},
		{"clip.m4a", []string{"audio/*"}, true},
		{"clip.mp4", []string{"audio/*"}, false},
		{"doc.pdf", []string{"application/pdf"}, true},
		{"anything.bin", nil, true},
		{"noext", []string{"image/*"}, false},
	}
	for _, c := range cases {
		if got := accepts(c.name, c.accept); got != c.want {
			t.Errorf("accepts(%q, %v) = %v, want %v", c.name, c.accept, got, c.want)
		}
	}
}

func TestFSFindAndFilter(t *testing.T) {
	ts := newTestServer(t)
	root := t.TempDir()
	for _, p := range []string{"a/lines-ep1.json", "a/b/lines-ep2.json", "a/b/lines-ep2.wav", "node_modules/lines.json", ".hidden/lines.json", "top.txt"} {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	var found struct {
		Entries []fsEntry
		Partial bool
	}
	ts.do(t, "GET", "/api/fs/find?files=1&accept=.json&q=LINES&path="+root, nil, 200, &found)
	var rels []string
	for _, e := range found.Entries {
		rels = append(rels, e.Rel)
	}
	if len(rels) != 2 || rels[0] != "a/lines-ep1.json" || rels[1] != "a/b/lines-ep2.json" {
		t.Fatalf("found %v", rels)
	}

	var list struct {
		Entries  []fsEntry
		Filtered int
	}
	ts.do(t, "GET", "/api/fs?files=1&accept=.json&path="+filepath.Join(root, "a", "b"), nil, 200, &list)
	if len(list.Entries) != 1 || list.Entries[0].Name != "lines-ep2.json" || list.Filtered != 1 {
		t.Fatalf("list %+v", list)
	}
}
