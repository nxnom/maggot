package config

import (
	"path/filepath"
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
