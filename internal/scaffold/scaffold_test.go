package scaffold

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/testkit"
)

func TestTemplatesPassTheirOwnTests(t *testing.T) {
	for _, l := range Langs() {
		t.Run(l, func(t *testing.T) {
			parent := t.TempDir()
			dir, err := Create(parent, Options{ID: "demo-" + l, Lang: l, Bin: "git"})
			if err != nil {
				t.Fatal(err)
			}
			m, err := manifest.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if m.Name != "Demo "+strings.ToUpper(l[:1])+l[1:] {
				t.Errorf("name %q", m.Name)
			}
			prog := m.Entry.Bin
			if prog == "" {
				prog = m.Entry.Interpreter[0]
			}
			if _, err := exec.LookPath(prog); err != nil {
				t.Skipf("%s not installed", prog)
			}
			s, err := testkit.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range testkit.Run(context.Background(), dir, m, s, testkit.Options{}) {
				if !r.Passed {
					t.Errorf("case %q failed: %v\nstdout: %s\nstderr: %s", r.Name, r.Failures, r.Stdout, r.Stderr)
				}
			}
		})
	}
}

func TestCreateErrors(t *testing.T) {
	parent := t.TempDir()
	if _, err := Create(parent, Options{ID: "nobin", Lang: "bin"}); err == nil || !strings.Contains(err.Error(), "--bin is required") {
		t.Fatalf("bin: %v", err)
	}
	if _, err := Create(parent, Options{ID: "x", Lang: "cobol"}); err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("lang: %v", err)
	}
	if _, err := Create(parent, Options{ID: "Bad_ID", Lang: "sh"}); err == nil {
		t.Fatal("invalid id should fail")
	}
	if _, err := Create(parent, Options{ID: "dup", Lang: "sh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(parent, Options{ID: "dup", Lang: "sh"}); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("dup: %v", err)
	}
}
