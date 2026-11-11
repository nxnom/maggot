package skill

import (
	"os"

	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

// TestFrontmatter parses the frontmatter with a strict YAML parser, like the
// skill installers do (an unquoted ": " in the description broke them).
func TestFrontmatter(t *testing.T) {
	if !strings.HasPrefix(Text, "---\n") {
		t.Fatal("SKILL.md must start with frontmatter")
	}
	end := strings.Index(Text[4:], "\n---\n")
	if end < 0 {
		t.Fatal("frontmatter not closed")
	}
	var fm map[string]any
	dec := yaml.NewDecoder(strings.NewReader(Text[4 : 4+end]))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}
	if fm["name"] != "maggot" {
		t.Fatalf("name = %v", fm["name"])
	}
	desc, _ := fm["description"].(string)
	if desc == "" || len(desc) > 1024 || !strings.Contains(desc, "search maggot first") {
		t.Fatalf("description (%d chars): %q", len(desc), desc)
	}
	if len(fm) != 2 {
		t.Fatalf("only name and description expected, got %v", fm)
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
