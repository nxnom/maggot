package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSkillScriptCommand(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string, mode os.FileMode) string {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	uvPy := write("scripts/a.py", "# /// script\n# dependencies = []\n# ///\nprint(1)\n", 0o644)
	plainPy := write("scripts/b.py", "print(1)\n", 0o644)
	sh := write("scripts/c.sh", "echo 1\n", 0o644)
	ts := write("scripts/d.ts", "console.log(1)\n", 0o644)
	bin := write("scripts/e", "#!/bin/sh\necho 1\n", 0o755)
	write("scripts/f.txt", "x", 0o644)
	ref := write("assets/g.sh", "echo 1\n", 0o644)
	all := func(string) (string, error) { return "/bin/x", nil }

	py := "python3"
	if got, _ := skillScriptCommand(dir, "b.py", all); len(got) == 2 {
		py = got[0]
	}
	cases := []struct {
		script string
		want   []string
	}{
		{"a.py", []string{"uv", "run", "--script", uvPy}},
		{"b.py", []string{py, plainPy}},
		{"c.sh", []string{"sh", sh}},
		{"d.ts", []string{"deno", "run", "-A", ts}},
		{"e", []string{bin}},
		{"scripts/c.sh", []string{"sh", sh}},
		{"assets/g.sh", []string{"sh", ref}},
	}
	for _, c := range cases {
		got, err := skillScriptCommand(dir, c.script, all)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v; want %v", c.script, got, err, c.want)
		}
	}
	for script, msg := range map[string]string{"f.txt": "don't know", "missing.py": "not found", "../x.py": "not found"} {
		if _, err := skillScriptCommand(dir, script, all); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: want error with %q, got %v", script, msg, err)
		}
	}
	none := func(string) (string, error) { return "", os.ErrNotExist }
	if _, err := skillScriptCommand(dir, "a.py", none); err == nil || !strings.Contains(err.Error(), "uv is not on PATH") {
		t.Errorf("missing uv: %v", err)
	}
}
