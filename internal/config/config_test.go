package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirFromEnv(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MAGGOT_CONFIG_DIR", d)
	got, err := Dir()
	if err != nil || got != d {
		t.Fatalf("Dir() = %q, %v", got, err)
	}
	t.Setenv("MAGGOT_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", d)
	if got, _ := Dir(); got != filepath.Join(d, "maggot") {
		t.Fatalf("Dir() = %q", got)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	c, err := LoadFile(filepath.Join(t.TempDir(), "config.json"))
	if err != nil || len(c.Toolboxes) != 0 {
		t.Fatalf("got %+v, %v", c, err)
	}
}

func TestAddRemoveDefaultRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	c, _ := LoadFile(path)
	a, b := t.TempDir(), t.TempDir()
	if err := c.Add("personal", a); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("team", b); err != nil {
		t.Fatal(err)
	}
	if c.Default != "personal" {
		t.Fatalf("first toolbox should be default, got %q", c.Default)
	}
	if err := c.Add("personal", t.TempDir()); err == nil {
		t.Fatal("duplicate name should fail")
	}
	if err := c.Add("other", a); err == nil {
		t.Fatal("duplicate path should fail")
	}
	if err := c.Add("Bad Name", t.TempDir()); err == nil {
		t.Fatal("invalid name should fail")
	}
	if err := c.SetDefault("team"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.Toolboxes) != 2 || c2.Default != "team" {
		t.Fatalf("round trip: %+v", c2)
	}
	if err := c2.Remove("team"); err != nil {
		t.Fatal(err)
	}
	if c2.Default != "personal" {
		t.Fatalf("default should fall back, got %q", c2.Default)
	}
	if err := c2.Remove("team"); err == nil {
		t.Fatal("removing twice should fail")
	}
}

func TestLoadStrictAndHome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	home, _ := os.UserHomeDir()

	os.WriteFile(path, []byte(`{"toolboxes":[{"name":"p","path":"~/tb"}],"default_toolbox":"p"}`), 0o644)
	c, err := LoadFile(path)
	if err != nil || c.Toolboxes[0].Path != filepath.Join(home, "tb") {
		t.Fatalf("home expansion: %+v %v", c, err)
	}
	os.WriteFile(path, []byte(`{"default":"p"}`), 0o644)
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("strict: %v", err)
	}
}

func TestProblems(t *testing.T) {
	good := t.TempDir()
	os.WriteFile(filepath.Join(good, "maggot.json"), []byte(`{"schema":1}`), 0o644)
	c := &Config{
		Toolboxes: []ToolboxRef{
			{Name: "ok", Path: good},
			{Name: "ok", Path: good},
			{Name: "Bad", Path: "relative/path"},
			{Name: "gone", Path: filepath.Join(good, "missing")},
		},
		Default: "nope",
	}
	got := strings.Join(c.Problems(), "\n")
	for _, want := range []string{"duplicate \"ok\"", "duplicate \"" + good, "invalid \"Bad\"", "must be absolute", "no maggot.json", "default_toolbox"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if p := (&Config{Toolboxes: []ToolboxRef{{Name: "ok", Path: good}}, Default: "ok"}).Problems(); len(p) != 0 {
		t.Errorf("valid config has problems: %v", p)
	}
}

func TestDataDirAndKeep(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MAGGOT_DATA_DIR", d)
	if got, _ := DataDir(); got != d {
		t.Fatalf("DataDir = %q", got)
	}
	t.Setenv("MAGGOT_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", d)
	if got, _ := DataDir(); got != filepath.Join(d, "maggot") {
		t.Fatalf("DataDir = %q", got)
	}
	if (&Config{}).Keep() != DefaultRunsKeep || (&Config{RunsKeep: 5}).Keep() != 5 {
		t.Fatal("Keep")
	}
}
