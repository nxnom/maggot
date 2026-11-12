package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/config"
)

func writeSkillDir(t *testing.T, dir, name, version, extra string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\nname: " + name + "\ndescription: Make notes from audio.\nmetadata:\n  version: " + version +
		"\n  maggot-uses: worker\n  maggot-env: NOTES_KEY\n---\n# Notes\n" + extra
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0o644)
	os.WriteFile(filepath.Join(dir, "scripts", "go.sh"), []byte("echo hi\n"), 0o755)
}

func TestSkillsOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NOTES_KEY", "")
	os.Unsetenv("NOTES_KEY")
	writeSkillDir(t, filepath.Join(ts.tb, "skills", "notes"), "notes", "1.0.0", "1. Listen.\n")

	var state struct {
		Skills []skillRow
	}
	ts.do(t, "GET", "/api/state", nil, 200, &state)
	if len(state.Skills) != 1 || state.Skills[0].Ref != "t/notes" || state.Skills[0].Ready ||
		strings.Join(state.Skills[0].Missing, ",") != "env NOTES_KEY" {
		t.Fatalf("state: %+v", state.Skills)
	}

	var sk struct {
		Kind, Ref, Body string
		Check           struct{ Ready bool }
		Agents          []agentRow
	}
	ts.do(t, "GET", "/api/skill?ref=t/notes", nil, 200, &sk)
	if sk.Kind != "skill" || sk.Body != "# Notes\n1. Listen.\n" || len(sk.Agents) != 3 || sk.Agents[0].Agent != "claude" {
		t.Fatalf("skill: %+v", sk)
	}

	// The shared file viewer works with kind=skill; SKILL.md comes first.
	var files struct{ Files []toolFile }
	ts.do(t, "GET", "/api/tool/files?kind=skill&ref=t/notes", nil, 200, &files)
	if len(files.Files) != 2 || files.Files[0].Path != "SKILL.md" {
		t.Fatalf("files: %+v", files)
	}
	var file struct{ Text string }
	ts.do(t, "GET", "/api/tool/file?kind=skill&ref=t/notes&path=scripts/go.sh", nil, 200, &file)
	if file.Text != "echo hi\n" {
		t.Fatalf("file: %q", file.Text)
	}
	ts.do(t, "GET", "/api/tool/file?kind=skill&ref=t/notes&path=../../tools/worker/tool.json", nil, 404, nil) // outside the folder

	// A secret makes it ready.
	ts.do(t, "POST", "/api/skill/secret", map[string]string{"ref": "t/notes", "name": "NOTES_KEY", "value": "k"}, 200, nil)
	ts.do(t, "POST", "/api/skill/secret", map[string]string{"ref": "t/notes", "name": "OTHER", "value": "k"}, 400, nil)
	ts.do(t, "GET", "/api/skill?ref=t/notes", nil, 200, &sk)
	if !sk.Check.Ready {
		t.Fatal("should be ready with the secret")
	}

	// Install into an agent: a link in the agent's skills folder.
	var ag struct{ Agents []agentRow }
	ts.do(t, "POST", "/api/skill/agents", map[string]any{"ref": "t/notes", "agent": "claude", "install": true}, 200, &ag)
	if ag.Agents[0].State != "linked" {
		t.Fatalf("agents: %+v", ag)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "notes", "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	// A draft of the same skill links to it and shows the changes.
	drafts, _ := config.SkillDraftsDir()
	writeSkillDir(t, filepath.Join(drafts, "notes"), "notes", "1.1.0", "1. Listen.\n2. Write.\n")
	ts.do(t, "GET", "/api/state", nil, 200, &state)
	if state.Skills[0].Draft == nil || state.Skills[0].Draft.Ref != "draft/notes" {
		t.Fatalf("draft link: %+v", state.Skills[0])
	}
	var changes struct {
		Of    toolLink
		Files []fileChange
	}
	ts.do(t, "GET", "/api/tool/changes?kind=skill&ref=draft/notes", nil, 200, &changes)
	if changes.Of.Ref != "t/notes" || len(changes.Files) != 1 || changes.Files[0].Path != "SKILL.md" {
		t.Fatalf("changes: %+v", changes)
	}
	var diff struct{ Lines []diffLine }
	ts.do(t, "GET", "/api/tool/diff?kind=skill&ref=draft/notes&path=SKILL.md", nil, 200, &diff)
	added := 0
	for _, l := range diff.Lines {
		if l.Op == "+" {
			added++
		}
	}
	if added != 2 { // version line and the new step
		t.Fatalf("diff: %+v", diff.Lines)
	}
	// Agents only get toolbox skills, not drafts.
	ts.do(t, "POST", "/api/skill/agents", map[string]any{"ref": "draft/notes", "agent": "claude", "install": true}, 400, nil)

	// Import the draft through the same import API as tools.
	var staged struct {
		Token   string
		Preview struct{ Kind, ID, Action string }
	}
	ts.do(t, "POST", "/api/import", map[string]string{"path": filepath.Join(drafts, "notes")}, 200, &staged)
	if staged.Preview.Kind != "skill" || staged.Preview.ID != "notes" || staged.Preview.Action != "upgrade" {
		t.Fatalf("staged: %+v", staged)
	}
	var done map[string]string
	ts.do(t, "POST", "/api/import/"+staged.Token, map[string]bool{}, 200, &done)
	if done["ref"] != "t/notes" || done["kind"] != "skill" {
		t.Fatalf("commit: %v", done)
	}
	ts.do(t, "DELETE", "/api/drafts?kind=skill&ref=draft/notes", nil, 200, nil)
	if _, err := os.Stat(filepath.Join(drafts, "notes")); !os.IsNotExist(err) {
		t.Fatal("draft not deleted")
	}

	// Search returns skills; deleting the skill removes the agent link too.
	var found struct{ Skills []string }
	ts.do(t, "GET", "/api/search?q=notes+audio", nil, 200, &found)
	if len(found.Skills) != 1 {
		t.Fatalf("search: %+v", found)
	}
	ts.do(t, "DELETE", "/api/tool?kind=skill&ref=t/notes", nil, 200, nil)
	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", "notes")); !os.IsNotExist(err) {
		t.Fatal("agent link should be removed")
	}
	if _, err := os.Stat(filepath.Join(ts.tb, "skills", "notes")); !os.IsNotExist(err) {
		t.Fatal("skill folder should be deleted")
	}
}
