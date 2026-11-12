package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func copyForTest(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(src, "SKILL.md"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, "SKILL.md"), b, 0o644)
}

func TestLinkAndUnlink(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "demo")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("x"), 0o644)

	if st, _, _ := State(home, "claude", "demo", src); st != StateNone {
		t.Fatalf("state %s", st)
	}
	p, st, err := Link(home, "claude", "demo", src, false, false, copyForTest)
	if err != nil || st != StateLinked || p != filepath.Join(home, ".claude", "skills", "demo") {
		t.Fatalf("link: %s %s %v", p, st, err)
	}
	if b, err := os.ReadFile(filepath.Join(p, "SKILL.md")); err != nil || string(b) != "x" {
		t.Fatal("link does not reach the skill")
	}
	// Linking again is a no-op; copying replaces the link with a copy.
	if _, st, _ := Link(home, "claude", "demo", src, false, false, copyForTest); st != StateLinked {
		t.Fatal(st)
	}
	if _, st, err := Link(home, "claude", "demo", src, true, false, copyForTest); err != nil || st != StateCopied {
		t.Fatalf("copy: %s %v", st, err)
	}
	if st, _, _ := State(home, "claude", "demo", src); st != StateCopied {
		t.Fatal(st)
	}
	if _, removed, err := Unlink(home, "claude", "demo", src, false); err != nil || !removed {
		t.Fatal("unlink copy")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source must stay")
	}

	// A folder maggot did not make is left alone without force.
	other := filepath.Join(home, ".codex", "skills", "demo")
	os.MkdirAll(other, 0o755)
	if st, _, _ := State(home, "codex", "demo", src); st != StateOther {
		t.Fatal(st)
	}
	if _, _, err := Link(home, "codex", "demo", src, false, false, copyForTest); err == nil {
		t.Fatal("should refuse")
	}
	if _, _, err := Unlink(home, "codex", "demo", src, false); err == nil {
		t.Fatal("should refuse")
	}
	if _, st, err := Link(home, "codex", "demo", src, false, true, copyForTest); err != nil || st != StateLinked {
		t.Fatalf("force: %s %v", st, err)
	}
	if _, removed, _ := Unlink(home, "codex", "demo", src, false); !removed {
		t.Fatal("unlink link")
	}
	if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
		t.Fatal("unlinking must not delete the source")
	}
}
