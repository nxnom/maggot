package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogTail(t *testing.T) {
	m := &Manager{Root: t.TempDir()}
	id := "20260101-000000-abcdef"
	dir := filepath.Join(m.Root, id)
	os.MkdirAll(dir, 0o755)
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&b, `{"t":"out","s":"line %d"}`+"\n", i)
		if i == 30 {
			b.WriteString(`{"t":"p","v":0.5}` + "\n")
		}
	}
	fmt.Fprintf(&b, `{"t":"err","s":"%s"}`+"\n", strings.Repeat("x", 5000))
	b.WriteString(`{"t":"out","s":"half a li`) // still being written
	os.WriteFile(filepath.Join(dir, "log.ndjson"), []byte(b.String()), 0o644)

	tail, err := m.LogTail(id, 3)
	if err != nil {
		t.Fatal(err)
	}
	if tail.Total != 51 || len(tail.Lines) != 3 || tail.Progress != 0.5 {
		t.Fatalf("got total %d, %d lines, progress %v", tail.Total, len(tail.Lines), tail.Progress)
	}
	if tail.Lines[0].Text != "line 49" || tail.Lines[0].Seq != 49 || tail.Lines[2].Type != "err" {
		t.Fatalf("bad lines: %+v", tail.Lines[:2])
	}
	if long := tail.Lines[2].Text; len(long) > maxLineChars+50 || !strings.Contains(long, "3000 more characters") {
		t.Fatalf("long line not cut: %d chars", len(long))
	}
	// Following from the offset picks up the rest once it is complete.
	f, _ := os.OpenFile(filepath.Join(dir, "log.ndjson"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("ne\"}\n")
	f.Close()
	more, _, err := m.LogFrom(id, tail.Offset)
	if err != nil || len(more) != 1 || more[0].Text != "half a line" {
		t.Fatalf("LogFrom: %+v %v", more, err)
	}
}
