package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoCopyMatchesEmbedded(t *testing.T) {
	repo, err := os.ReadFile(filepath.Join("..", "..", "skills", Name, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(repo) != Text {
		t.Fatal("skills/maggot/SKILL.md differs from internal/skill/SKILL.md; copy it over")
	}
}

func TestFrontmatter(t *testing.T) {
	if !strings.HasPrefix(Text, "---\nname: maggot\ndescription: ") {
		t.Fatal("SKILL.md must start with name and description frontmatter")
	}
	end := strings.Index(Text[4:], "\n---\n")
	if end < 0 {
		t.Fatal("frontmatter not closed")
	}
	desc := Text[4 : 4+end]
	if len(desc) > 1024 {
		t.Fatalf("frontmatter too long: %d", len(desc))
	}
}

func TestInstallUninstall(t *testing.T) {
	home := t.TempDir()
	p, changed, err := Install(home, "claude")
	if err != nil || !changed || p != filepath.Join(home, ".claude", "skills", "maggot", "SKILL.md") {
		t.Fatalf("install: %s %v %v", p, changed, err)
	}
	if _, changed, _ := Install(home, "claude"); changed {
		t.Fatal("second install should be a no-op")
	}
	os.WriteFile(p, []byte("old"), 0o644)
	if _, changed, _ := Install(home, "claude"); !changed {
		t.Fatal("outdated skill should be rewritten")
	}
	if _, _, err := Install(home, "emacs"); err == nil {
		t.Fatal("unknown agent should fail")
	}
	if _, err := Uninstall(home, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
		t.Fatal("skill folder should be gone")
	}
}
