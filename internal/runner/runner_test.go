package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/manifest"
)

const testManifest = `{
  "schema": 1, "id": "conv", "version": "2.0.0", "name": "Conv", "description": "d",
  "entry": {"path": "run.py", "interpreter": ["python3"]},
  "inputs": [
    {"name": "width", "type": "integer", "min": 1, "default": 800, "arg": {"flag": "--width"}},
    {"name": "scale", "type": "number", "arg": {"flag": "--scale"}},
    {"name": "format", "type": "enum", "options": ["png", "webp"], "arg": {"flag": "-f"}},
    {"name": "force", "type": "boolean", "arg": {"flag": "--force"}},
    {"name": "extra", "type": "files", "arg": {"flag": "--extra", "repeat": true}},
    {"name": "title", "type": "string", "arg": {"flag": "--title"}},
    {"name": "src", "type": "file", "required": true, "arg": {"positional": true}},
    {"name": "more", "type": "files", "arg": {"positional": true}}
  ],
  "outputs": {"dir_flag": "--out"}
}`

func load(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(testManifest))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func values(t *testing.T, js string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestBuildArgs(t *testing.T) {
	m := load(t)
	cases := []struct {
		name, values, out string
		want              []string
		err               string
	}{
		{name: "defaults", values: `{"src": "a.png"}`, want: []string{"--width", "800", "a.png"}},
		{name: "everything", out: "/tmp/o",
			values: `{"width": 10, "scale": 1.5, "format": "webp", "force": true, "extra": ["x", "y"], "title": "a b", "src": "a.png", "more": ["b.png", "c.png"]}`,
			want:   []string{"--width", "10", "--scale", "1.5", "-f", "webp", "--force", "--extra", "x", "--extra", "y", "--title", "a b", "--out", "/tmp/o", "a.png", "b.png", "c.png"}},
		{name: "false bool and empty fields", values: `{"force": false, "format": "", "more": [], "src": "a"}`, want: []string{"--width", "800", "a"}},
		{name: "empty string kept for string type", values: `{"title": "", "src": "a"}`, want: []string{"--width", "800", "--title", "", "a"}},
		{name: "null uses default", values: `{"width": null, "src": "a"}`, want: []string{"--width", "800", "a"}},
		{name: "missing required", values: `{}`, err: "src: is required"},
		{name: "unknown", values: `{"src": "a", "colour": 1}`, err: "unknown input(s): colour"},
		{name: "bad enum", values: `{"src": "a", "format": "gif"}`, err: "format:"},
		{name: "below min", values: `{"src": "a", "width": 0}`, err: "width: must be >= 1"},
		{name: "wrong type", values: `{"src": "a", "force": "yes"}`, err: "force: must be true or false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildArgs(m, values(t, tc.values), tc.out)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("want error %q, got %v", tc.err, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestBuildArgsOutDirWithoutFlag(t *testing.T) {
	m := load(t)
	m.Outputs = nil
	if _, err := BuildArgs(m, values(t, `{"src":"a"}`), "/o"); err == nil {
		t.Fatal("want error")
	}
}

func TestCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"$MAGGOT_TOOL_ID $MAGGOT_TOOL_VERSION $MAGGOT_EVENTS $EXTRA $*\"\npwd\n"
	os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755)
	m := &manifest.Manifest{ID: "t", Version: "1.0.0", Entry: manifest.Entry{Path: "run"}}
	work := t.TempDir()
	cmd, err := Command(dir, m, []string{"a", "b c"}, Options{Dir: work, ExtraEnv: []string{"EXTRA=x"}, EventsPath: "/ev"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if lines[0] != "t 1.0.0 /ev x a b c" {
		t.Fatalf("got %q", lines[0])
	}
	realWork, _ := filepath.EvalSymlinks(work)
	if realPwd, _ := filepath.EvalSymlinks(lines[1]); realPwd != realWork {
		t.Fatalf("cwd %q want %q", lines[1], work)
	}
}

func TestArgvWithInterpreter(t *testing.T) {
	m := &manifest.Manifest{Entry: manifest.Entry{Path: "run.py", Interpreter: []string{"uv", "run", "--script"}}}
	argv, err := Argv("/tools/x", m, []string{"--a"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"uv", "run", "--script", filepath.Join("/tools/x", "run.py"), "--a"}
	if runtime.GOOS != "windows" && !reflect.DeepEqual(argv, want) {
		t.Fatalf("got %q", argv)
	}
}
