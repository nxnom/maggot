package toolbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTool creates a minimal valid tool folder.
func writeTool(t *testing.T, tb *Toolbox, folder, id string) {
	t.Helper()
	dir := filepath.Join(tb.ToolsPath(), folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := fmt.Sprintf(`{"schema":1,"id":%q,"version":"1.0.0","name":"T","description":"d","entry":{"path":"run"}}`, id)
	if err := os.WriteFile(filepath.Join(dir, "tool.json"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestInitOpen(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, "mine"); err == nil {
		t.Fatal("second init should fail")
	}
	tb, err := Open(dir, "mine")
	if err != nil || tb.Meta.Name != "mine" {
		t.Fatalf("open: %+v %v", tb, err)
	}
	if _, err := Open(t.TempDir(), "x"); err == nil || !strings.Contains(err.Error(), "not a toolbox") {
		t.Fatalf("want not a toolbox, got %v", err)
	}
}

func TestToolsAndProblems(t *testing.T) {
	tb, _ := Init(t.TempDir(), "a")
	writeTool(t, tb, "zeta", "zeta")
	writeTool(t, tb, "alpha", "alpha")
	writeTool(t, tb, "wrong-folder", "other-id")
	if err := os.MkdirAll(filepath.Join(tb.ToolsPath(), "not-a-tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(tb.ToolsPath(), "broken")
	os.MkdirAll(broken, 0o755)
	os.WriteFile(filepath.Join(broken, "tool.json"), []byte("{"), 0o644)

	tools, problems := tb.Tools()
	if len(tools) != 2 || tools[0].Manifest.ID != "alpha" || tools[1].Ref() != "a/zeta" {
		t.Fatalf("tools: %v", tools)
	}
	if len(problems) != 2 {
		t.Fatalf("problems: %+v", problems)
	}
}

func TestResolve(t *testing.T) {
	a, _ := Init(t.TempDir(), "a")
	b, _ := Init(t.TempDir(), "b")
	writeTool(t, a, "shared", "shared")
	writeTool(t, b, "shared", "shared")
	writeTool(t, a, "image-resize", "image-resize")
	s := &Set{Toolboxes: []*Toolbox{a, b}}

	if tool, err := s.Resolve("image-resize"); err != nil || tool.Ref() != "a/image-resize" {
		t.Fatalf("resolve: %v %v", tool, err)
	}
	if _, err := s.Resolve("shared"); err == nil || !strings.Contains(err.Error(), "a/shared, b/shared") {
		t.Fatalf("want ambiguity error, got %v", err)
	}
	if tool, err := s.Resolve("b/shared"); err != nil || tool.Toolbox != b {
		t.Fatalf("qualified: %v %v", tool, err)
	}
	if _, err := s.Resolve("resize"); err == nil || !strings.Contains(err.Error(), "did you mean: a/image-resize") {
		t.Fatalf("want suggestion, got %v", err)
	}
}

func TestFindUp(t *testing.T) {
	tb, _ := Init(t.TempDir(), "x")
	deep := filepath.Join(tb.Path, "tools", "a", "b")
	os.MkdirAll(deep, 0o755)
	got, ok := FindUp(deep)
	want, _ := filepath.EvalSymlinks(tb.Path)
	gotReal, _ := filepath.EvalSymlinks(got)
	if !ok || gotReal != want {
		t.Fatalf("FindUp = %q %v, want %q", got, ok, want)
	}
}

func TestSkills(t *testing.T) {
	dir := t.TempDir()
	tb, err := Init(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	write := func(folder, text string) {
		p := filepath.Join(dir, SkillsDir, folder)
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, "SKILL.md"), []byte(text), 0o644)
	}
	write("good", "---\nname: good\ndescription: A good skill.\n---\nsteps\n")
	write("renamed", "---\nname: other\ndescription: x\n---\n")
	os.MkdirAll(filepath.Join(dir, SkillsDir, "empty"), 0o755)

	set := &Set{Toolboxes: []*Toolbox{tb}}
	skills, problems := set.Skills()
	if len(skills) != 1 || skills[0].Ref() != "main/good" || len(problems) != 1 {
		t.Fatalf("skills %v problems %v", skills, problems)
	}
	if sk, err := set.ResolveSkill("good"); err != nil || sk.Body != "steps\n" {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := set.ResolveSkill("main/good"); err != nil {
		t.Fatal(err)
	}
	if _, err := set.ResolveSkill("renamed"); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("broken: %v", err)
	}
	if _, err := set.ResolveSkill("goo"); err == nil || !strings.Contains(err.Error(), "did you mean: main/good") {
		t.Fatalf("similar: %v", err)
	}
	// Skill problems do not make a tool of the same name look broken.
	if _, err := set.Resolve("renamed"); err == nil || strings.Contains(err.Error(), "broken") {
		t.Fatalf("tool resolve: %v", err)
	}
}
