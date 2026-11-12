package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiffLines(t *testing.T) {
	a := strings.Split("a b c d e f g h i j", " ")
	b := strings.Split("a b c D e f g h i j k", " ")
	got, tooBig := diffLines(a, b, 1)
	if tooBig {
		t.Fatal("too big")
	}
	var ops []string
	for _, l := range got {
		if l.Op == "~" {
			ops = append(ops, "~"+strings.Repeat(".", l.Skip))
		} else {
			ops = append(ops, l.Op+l.Text)
		}
	}
	// a and b stay: hiding two lines saves nothing. f..i are hidden.
	want := []string{" a", " b", " c", "-d", "+D", " e", "~....", " j", "+k"}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("got %q, want %q", ops, want)
	}
	// Line numbers follow each side.
	if got[2].Old != 3 || got[2].New != 3 || got[3].Old != 4 || got[4].New != 4 || got[8].New != 11 {
		t.Fatalf("bad line numbers: %+v", got)
	}
}

func TestDiffLinesAddedFile(t *testing.T) {
	got, _ := diffLines(nil, []string{"x", "y"}, 3)
	if len(got) != 2 || got[0].Op != "+" || got[1].New != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestCompareDirs(t *testing.T) {
	old, nw := t.TempDir(), t.TempDir()
	write := func(dir, name, body string) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(old, "tool.json", "{}")
	write(nw, "tool.json", "{}")
	write(old, "run.sh", "echo 1")
	write(nw, "run.sh", "echo 2")
	write(old, "gone.txt", "x")
	write(nw, "tests/new.json", "[]")
	write(nw, "node_modules/x.js", "skipped")
	got, err := compareDirs(old, nw)
	if err != nil {
		t.Fatal(err)
	}
	want := []fileChange{{"gone.txt", "removed"}, {"run.sh", "changed"}, {"tests/new.json", "added"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
