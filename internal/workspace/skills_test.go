package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/secrets"
	"github.com/nxnom/maggot/internal/toolbox"
)

func TestCheckSkill(t *testing.T) {
	dir := t.TempDir()
	tb, err := toolbox.Init(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	td := filepath.Join(tb.ToolsPath(), "say")
	os.MkdirAll(td, 0o755)
	os.WriteFile(filepath.Join(td, "tool.json"), []byte(`{"schema":1,"id":"say","version":"1.0.0","name":"Say","description":"d","entry":{"path":"run"},"requires":[{"bin":"espeak"}]}`), 0o644)
	os.WriteFile(filepath.Join(td, "run"), []byte("#!/bin/sh\n"), 0o755)
	set := &toolbox.Set{Toolboxes: []*toolbox.Toolbox{tb}}
	sk := &toolbox.Skill{Toolbox: tb, Skill: &agentskill.Skill{Meta: agentskill.Meta{Name: "demo"},
		Uses: []string{"say", "nope"}, Requires: []string{"ffmpeg", "zzz"}, Env: []string{"DEMO_TOKEN"}}}
	have := map[string]bool{"ffmpeg": true}
	look := func(bin string) (string, error) {
		if have[bin] {
			return "/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}
	store := &secrets.FileStore{Path: filepath.Join(t.TempDir(), "s.json")}
	t.Setenv("DEMO_TOKEN", "")
	os.Unsetenv("DEMO_TOKEN")

	c := CheckSkill(set, sk, store, look)
	want := []string{"tool say (needs binary espeak)", "tool nope", "binary zzz", "env DEMO_TOKEN"}
	if c.Ready || !reflect.DeepEqual(c.Missing, want) {
		t.Fatalf("missing %q", c.Missing)
	}
	if !c.Tools[0].Found || c.Tools[0].Ref != "main/say" || c.Tools[1].Found || !c.Requires[0].Found {
		t.Fatalf("details %+v", c)
	}

	// A stored secret under the skill's own key counts; a tool's does not.
	store.Set("demo", "DEMO_TOKEN", "x")
	if c := CheckSkill(set, sk, store, look); c.Env[0].Set {
		t.Fatal("tool key must not count")
	}
	store.Set(SkillSecretKey("demo"), "DEMO_TOKEN", "x")
	have["espeak"], have["zzz"] = true, true
	sk.Uses = []string{"say"}
	if c := CheckSkill(set, sk, store, look); !c.Ready || c.Env[0].Source != "store" {
		t.Fatalf("ready: %+v", c)
	}
	if env, _ := SkillSecretEnv(store, sk); !reflect.DeepEqual(env, []string{"DEMO_TOKEN=x"}) {
		t.Fatalf("env %q", env)
	}
}
