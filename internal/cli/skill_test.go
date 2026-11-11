package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillInstall(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	code, out, _ := run(t, "skill", "install")
	if code != 0 || !strings.Contains(out, "claude:") || !strings.Contains(out, "codex:") {
		t.Fatalf("install: %s", out)
	}
	for _, rel := range []string{".claude/skills/maggot/SKILL.md", ".codex/skills/maggot/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Fatal(err)
		}
	}
	if _, out, _ := run(t, "skill", "install", "--agent", "agents,claude"); !strings.Contains(out, "claude: ") || !strings.Contains(out, "up to date") {
		t.Fatalf("second install: %s", out)
	}
	if code, _, e := run(t, "skill", "install", "--agent", "vim"); code == 0 || !strings.Contains(e, "unknown agent") {
		t.Fatalf("bad agent: %s", e)
	}
	run(t, "skill", "uninstall", "--agent", "codex")
	if _, err := os.Stat(filepath.Join(home, ".codex/skills/maggot")); !os.IsNotExist(err) {
		t.Fatal("uninstall failed")
	}
	if _, out, _ := run(t, "skill", "show"); !strings.HasPrefix(out, "---\nname: maggot") {
		t.Fatal("show")
	}
}
