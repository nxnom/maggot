package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/secrets"
)

const demoSkill = `---
name: clip-maker
description: Cut a short audio clip from a video link. Use when the user wants a ringtone or sample.
metadata:
  version: 1.0.0
  maggot-uses: hello
  maggot-requires: sh
  maggot-env: CLIP_TOKEN
---
# Clip maker

1. Run scripts/show.sh.
`

func TestSkillLifecycle(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLIP_TOKEN", "")
	os.Unsetenv("CLIP_TOKEN")
	tb := newToolbox(t, "main")
	addTool(t, tb, "hello", helloManifest, helloScript)
	store := &secrets.FileStore{Path: filepath.Join(t.TempDir(), "s.json")}
	setup := func(a *app) { a.secrets = store }

	// New skill goes to the skill drafts folder.
	code, out, e := run(t, "skill", "new", "clip-maker", "--description", "Cut clips: fast.")
	if code != 0 {
		t.Fatal(e)
	}
	draft := filepath.Join(os.Getenv("MAGGOT_DATA_DIR"), "drafts", "skills", "clip-maker")
	if !strings.Contains(out, draft) {
		t.Fatalf("new: %s", out)
	}
	if code, _, e := run(t, "skill", "new", "Bad_Name"); code == 0 || !strings.Contains(e, "lowercase") {
		t.Fatalf("bad name: %s", e)
	}
	os.WriteFile(filepath.Join(draft, "SKILL.md"), []byte(demoSkill), 0o644)
	os.MkdirAll(filepath.Join(draft, "scripts"), 0o755)
	os.WriteFile(filepath.Join(draft, "scripts", "show.sh"), []byte("#!/bin/sh\necho \"token=$CLIP_TOKEN\"\n"), 0o755)

	// A draft can be checked by path before it is imported.
	if code, out, _ := runApp(t, "", setup, "skill", "show", draft); code != 0 || !strings.Contains(out, "(draft/clip-maker)") || !strings.Contains(out, "missing env CLIP_TOKEN") {
		t.Fatalf("show draft: %s", out)
	}

	if code, out, e := run(t, "import", "--yes", draft); code != 0 || !strings.Contains(out, "installed skill main/clip-maker 1.0.0") {
		t.Fatalf("import: %s %s", out, e)
	}
	if _, err := os.Stat(filepath.Join(tb, "skills", "clip-maker", "scripts", "show.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(draft); !os.IsNotExist(err) {
		t.Fatal("draft should be deleted after import")
	}

	// list and search show the skill next to tools.
	_, out, _ = run(t, "list", "--json")
	var list struct {
		Tools  []struct{ Ref string }
		Skills []skillSummary
	}
	if json.Unmarshal([]byte(out), &list) != nil || len(list.Tools) != 1 || len(list.Skills) != 1 || list.Skills[0].Ref != "main/clip-maker" {
		t.Fatalf("list: %s", out)
	}
	_, out, _ = run(t, "search", "ringtone", "--json")
	var found struct {
		Results []struct{ Ref string }
		Skills  []struct {
			Ref     string
			Matched []string
		}
	}
	if json.Unmarshal([]byte(out), &found) != nil || len(found.Skills) != 1 || found.Skills[0].Ref != "main/clip-maker" || len(found.Results) != 0 {
		t.Fatalf("search: %s", out)
	}
	if _, out, _ := run(t, "search", "ringtone"); !strings.Contains(out, "SKILL") || !strings.Contains(out, "maggot skill show") {
		t.Fatalf("search text: %s", out)
	}

	// Secrets for a skill are stored under the skill and passed by exec.
	if code, _, e := runApp(t, "tok\n", setup, "secret", "set", "clip-maker", "CLIP_TOKEN"); code != 0 {
		t.Fatal(e)
	}
	if v, ok, _ := store.Get("skill:clip-maker", "CLIP_TOKEN"); !ok || v != "tok" {
		t.Fatal("secret not stored under the skill")
	}
	_, out, _ = runApp(t, "", setup, "skill", "show", "clip-maker", "--json")
	var shown struct {
		Dir   string
		Check struct {
			Ready   bool
			Missing []string
		}
		Files []string
		Body  string
	}
	if json.Unmarshal([]byte(out), &shown) != nil || !shown.Check.Ready || len(shown.Files) != 1 || !strings.HasPrefix(shown.Body, "# Clip maker") {
		t.Fatalf("show json: %s", out)
	}
	if code, out, e := runApp(t, "", setup, "skill", "exec", "clip-maker", "--", "sh", filepath.Join(shown.Dir, "scripts", "show.sh")); code != 0 || out != "token=tok\n" {
		t.Fatalf("exec: %d %q %s", code, out, e)
	}
	if code, _, _ := runApp(t, "", setup, "skill", "exec", "clip-maker", "--", "sh", "-c", "exit 4"); code != 4 {
		t.Fatalf("exit code %d", code)
	}

	// Link into agents, then remove the skill: the links go too.
	if code, out, e := run(t, "skill", "install", "clip-maker"); code != 0 || !strings.Contains(out, "(linked)") {
		t.Fatalf("install: %s %s", out, e)
	}
	link := filepath.Join(home, ".claude", "skills", "clip-maker")
	if b, err := os.ReadFile(filepath.Join(link, "SKILL.md")); err != nil || !strings.Contains(string(b), "clip-maker") {
		t.Fatal("link")
	}
	if code, _, _ := run(t, "skill", "draft", "clip-maker"); code != 0 {
		t.Fatal("draft")
	}
	if code, out, _ := run(t, "export", "clip-maker", "-o", t.TempDir()); code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "clip-maker-1.0.0.maggot") {
		t.Fatalf("export: %s", out)
	}
	if code, out, _ := run(t, "remove", "--yes", "clip-maker"); code != 0 || !strings.Contains(out, "removed skill main/clip-maker") {
		t.Fatalf("remove: %s", out)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("link should be removed with the skill")
	}
	// maggot's own skill still works with no names.
	if _, out, _ := run(t, "skill", "show"); !strings.HasPrefix(out, "---\nname: maggot") {
		t.Fatal("own skill")
	}
}
