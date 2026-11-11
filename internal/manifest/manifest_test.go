package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const validJSON = `{
  "schema": 1,
  "id": "image-resize",
  "version": "1.2.0",
  "name": "Image Resize",
  "description": "Resize images.",
  "categories": ["Images"],
  "tags": ["resize", "thumbnail"],
  "entry": {"path": "run.py", "interpreter": ["uv", "run", "--script"]},
  "requires": [
    {"bin": "uv", "version": ">=0.5", "install": {"brew": "uv"}},
    {"bin": "magick", "version": ">=7", "check": ["-version"], "install": {"brew": "imagemagick", "apt": "imagemagick"}}
  ],
  "env": [{"name": "API_KEY", "secret": true}],
  "inputs": [
    {"name": "width", "type": "integer", "min": 1, "max": 8000, "default": 800, "arg": {"flag": "--width"}},
    {"name": "format", "type": "enum", "options": ["png", {"value": "jpeg", "label": "JPEG"}], "default": "png", "arg": {"flag": "--format"}},
    {"name": "force", "type": "boolean", "arg": {"flag": "--force"}},
    {"name": "files", "type": "files", "required": true, "accept": ["image/*"], "arg": {"positional": true}}
  ],
  "outputs": {"kind": "files", "dir_flag": "--out"},
  "examples": ["run --width 800 a.png"]
}`

func TestParseValid(t *testing.T) {
	m, err := Parse([]byte(validJSON))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "image-resize" || len(m.Inputs) != 4 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if got := m.Inputs[1].Options; got[0].Value != "png" || got[1].Label != "JPEG" {
		t.Fatalf("options: %+v", got)
	}
}

// mutate parses the valid manifest into a generic map, applies fn and re-encodes.
func mutate(t *testing.T, fn func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(validJSON), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func input(m map[string]any, i int) map[string]any {
	return m["inputs"].([]any)[i].(map[string]any)
}

func TestParseInvalid(t *testing.T) {
	cases := []struct {
		name string
		fn   func(m map[string]any)
		want string
	}{
		{"schema", func(m map[string]any) { m["schema"] = 2 }, "schema: must be 1"},
		{"id", func(m map[string]any) { m["id"] = "Image_Resize" }, "id: must be lowercase"},
		{"version", func(m map[string]any) { m["version"] = "1.2" }, "version: must be semver"},
		{"name", func(m map[string]any) { m["name"] = " " }, "name: is required"},
		{"empty tag", func(m map[string]any) { m["tags"] = []any{""} }, "tags[0]"},
		{"unknown field", func(m map[string]any) { m["colour"] = "red" }, "unknown field"},
		{"dup category", func(m map[string]any) { m["categories"] = []any{"A", "a"} }, "duplicate"},
		{"entry escape", func(m map[string]any) { m["entry"] = map[string]any{"path": "../x"} }, "entry.path"},
		{"entry abs", func(m map[string]any) { m["entry"] = map[string]any{"path": "/bin/sh"} }, "entry.path"},
		{"entry backslash", func(m map[string]any) { m["entry"] = map[string]any{"path": `a\b`} }, "forward slashes"},
		{"bad bin", func(m map[string]any) { m["requires"].([]any)[0].(map[string]any)["bin"] = "rm -rf" }, "invalid binary"},
		{"bad constraint", func(m map[string]any) { m["requires"].([]any)[0].(map[string]any)["version"] = ">>1" }, "invalid constraint"},
		{"unknown pm", func(m map[string]any) {
			m["requires"].([]any)[0].(map[string]any)["install"] = map[string]any{"brw": "uv"}
		}, "unknown package manager"},
		{"pkg injection", func(m map[string]any) {
			m["requires"].([]any)[0].(map[string]any)["install"] = map[string]any{"brew": "uv; rm -rf /"}
		}, "invalid package name"},
		{"env reserved", func(m map[string]any) { m["env"] = []any{map[string]any{"name": "MAGGOT_X"}} }, "reserved"},
		{"env case", func(m map[string]any) { m["env"] = []any{map[string]any{"name": "api_key"}} }, "UPPER_SNAKE_CASE"},
		{"input type", func(m map[string]any) { input(m, 0)["type"] = "color" }, "unknown type"},
		{"dup input", func(m map[string]any) { input(m, 1)["name"] = "width" }, "duplicate \"width\""},
		{"enum no options", func(m map[string]any) { delete(input(m, 1), "options"); delete(input(m, 1), "default") }, "at least one option"},
		{"default out of range", func(m map[string]any) { input(m, 0)["default"] = 9000 }, "must be <= 8000"},
		{"default not int", func(m map[string]any) { input(m, 0)["default"] = 1.5 }, "whole number"},
		{"enum default", func(m map[string]any) { input(m, 1)["default"] = "gif" }, "not one of the options"},
		{"min on string", func(m map[string]any) { input(m, 1)["min"] = 1 }, "min/max only"},
		{"no arg", func(m map[string]any) { input(m, 0)["arg"] = map[string]any{} }, "needs a flag or positional"},
		{"both arg", func(m map[string]any) { input(m, 0)["arg"] = map[string]any{"flag": "--w", "positional": true} }, "not both"},
		{"dup flag", func(m map[string]any) { input(m, 2)["arg"] = map[string]any{"flag": "--width"} }, "duplicate flag"},
		{"flag clashes with out", func(m map[string]any) { input(m, 2)["arg"] = map[string]any{"flag": "--out"} }, "duplicate flag"},
		{"bool positional", func(m map[string]any) { input(m, 2)["arg"] = map[string]any{"positional": true} }, "boolean inputs must use a flag"},
		{"after variadic", func(m map[string]any) {
			m["inputs"] = append(m["inputs"].([]any), map[string]any{"name": "dest", "type": "string", "arg": map[string]any{"positional": true}})
		}, "may follow the list input"},
		{"files flag no repeat", func(m map[string]any) { input(m, 3)["arg"] = map[string]any{"flag": "--in"} }, "need repeat"},
		{"warm without args", func(m map[string]any) { m["warm"] = map[string]any{"args": []any{}} }, "warm.args"},
		{"no kind", func(m map[string]any) { m["outputs"] = map[string]any{"dir_flag": "--out"} }, "outputs.kind"},
		{"bad kind", func(m map[string]any) { m["outputs"] = map[string]any{"kind": "video"} }, "outputs.kind"},
		{"dir flag on stdout", func(m map[string]any) { m["outputs"] = map[string]any{"kind": "stdout", "dir_flag": "--o"} }, "only allowed with kind \"files\""},
		{"format on files", func(m map[string]any) { m["outputs"] = map[string]any{"kind": "files", "format": "json"} }, "only allowed with kind \"stdout\""},
		{"bad format", func(m map[string]any) { m["outputs"] = map[string]any{"kind": "stdout", "format": "xml"} }, "text or json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(mutate(t, tc.fn))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidationErrorListsAllProblems(t *testing.T) {
	_, err := Parse([]byte(`{"schema": 1, "id": "X", "version": "x", "name": "", "description": "", "entry": {"path": ""}}`))
	if !IsValidationError(err) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	for _, want := range []string{"id:", "version:", "name:", "description:", "entry:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestLoadChecksEntryFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"schema":1,"id":"hello","version":"0.1.0","name":"Hello","description":"Say hi.","entry":{"path":"run"}}`
	write(FileName, manifest, 0o644)

	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want missing entry error, got %v", err)
	}
	write("run", "#!/bin/sh\necho hi\n", 0o644)
	if runtime.GOOS != "windows" {
		if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "not executable") {
			t.Fatalf("want not executable error, got %v", err)
		}
	}
	if err := os.Chmod(filepath.Join(dir, "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := m.EntryPath(dir)
	if !filepath.IsAbs(p) || filepath.Base(p) != "run" {
		t.Fatalf("entry path %q", p)
	}
}

func TestOptionRejectsUnknownFields(t *testing.T) {
	var o Option
	if err := json.Unmarshal([]byte(`{"value":"a","lable":"A"}`), &o); err == nil {
		t.Fatal("want error for misspelled field")
	}
}

const binJSON = `{
  "schema": 1, "id": "yt", "version": "1.0.0", "name": "YT", "description": "d",
  "entry": {"bin": "yt-dlp", "args": ["--newline", "-o", "%(title)s.%(ext)s"]},
  "inputs": [
    {"name": "urls", "type": "strings", "required": true, "placeholder": "https://", "arg": {"positional": true}},
    {"name": "mode", "type": "enum", "default": "video", "options": [
      {"value": "video", "args": []},
      {"value": "mp3", "label": "Audio MP3", "args": ["-x", "--audio-format", "mp3"]}
    ]},
    {"name": "max_height", "type": "enum", "options": ["1080", "720"], "arg": {"flag": "-S", "template": "res:{value}"}},
    {"name": "subs", "type": "boolean", "group": "Extras", "arg": {"args": ["--write-subs", "--embed-subs"]}},
    {"name": "extra", "type": "args", "advanced": true, "default": "--limit-rate '2M'"}
  ],
  "outputs": {"kind": "files", "dir_flag": "-P", "progress": {"regex": "\\[download\\]\\s+([\\d.]+)%"}}
}`

func TestBinManifest(t *testing.T) {
	m, err := Parse([]byte(binJSON))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Inputs[1].ChoiceArgs() || m.Inputs[2].ChoiceArgs() {
		t.Fatal("ChoiceArgs")
	}
	// A bin tool has no files to check.
	if err := m.CheckFiles(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// "args": [] must survive a JSON round trip (it means "emit nothing").
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `{"value":"video","args":[]}`) || !strings.Contains(string(b), `{"value":"1080"}`) {
		t.Fatalf("option JSON: %s", b)
	}
	if _, err := Parse(b); err != nil {
		t.Fatalf("round trip: %v", err)
	}
}

func TestBinManifestInvalid(t *testing.T) {
	mut := func(fn func(m map[string]any)) []byte {
		var m map[string]any
		json.Unmarshal([]byte(binJSON), &m)
		fn(m)
		b, _ := json.Marshal(m)
		return b
	}
	cases := []struct {
		name string
		fn   func(m map[string]any)
		want string
	}{
		{"path and bin", func(m map[string]any) { m["entry"] = map[string]any{"bin": "x", "path": "run"} }, "not both"},
		{"bin with interpreter", func(m map[string]any) {
			m["entry"] = map[string]any{"bin": "x", "interpreter": []any{"sh"}}
		}, "only allowed with entry.path"},
		{"bad bin", func(m map[string]any) { m["entry"] = map[string]any{"bin": "a b"} }, "invalid program name"},
		{"mixed option args", func(m map[string]any) {
			input(m, 1)["options"] = []any{map[string]any{"value": "a", "args": []any{}}, "b"}
		}, "every option has args or none"},
		{"choice args with flag", func(m map[string]any) { input(m, 1)["arg"] = map[string]any{"flag": "--mode"} }, "not used for this input"},
		{"args input with flag", func(m map[string]any) { input(m, 4)["arg"] = map[string]any{"flag": "--x"} }, "not used for this input"},
		{"bad args default", func(m map[string]any) { input(m, 4)["default"] = "'open" }, "unclosed quote"},
		{"template without value", func(m map[string]any) { input(m, 2)["arg"] = map[string]any{"flag": "-S", "template": "res"} }, "must contain {value}"},
		{"bool flag and args", func(m map[string]any) {
			input(m, 3)["arg"] = map[string]any{"flag": "--s", "args": []any{"--a"}}
		}, "either flag or args"},
		{"args on non-bool", func(m map[string]any) {
			input(m, 2)["arg"] = map[string]any{"flag": "-S", "args": []any{"--a"}}
		}, "only allowed for boolean"},
		{"strings flag without repeat", func(m map[string]any) { input(m, 0)["arg"] = map[string]any{"flag": "--url"} }, "need repeat"},
		{"progress no group", func(m map[string]any) {
			m["outputs"] = map[string]any{"kind": "files", "progress": map[string]any{"regex": "\\d+%"}}
		}, "needs a group"},
		{"progress bad regex", func(m map[string]any) {
			m["outputs"] = map[string]any{"kind": "files", "progress": map[string]any{"regex": "("}}
		}, "invalid regex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(mut(tc.fn))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		``:                                nil,
		`--limit-rate 2M`:                 {"--limit-rate", "2M"},
		`  a   b  `:                       {"a", "b"},
		`-o '%(title)s [%(id)s].%(ext)s'`: {"-o", "%(title)s [%(id)s].%(ext)s"},
		`--x "a \"q\" b"`:                 {"--x", `a "q" b`},
		`a\ b c`:                          {"a b", "c"},
		`''`:                              {""},
	}
	for in, want := range cases {
		got, err := SplitArgs(in)
		if err != nil || strings.Join(got, "|") != strings.Join(want, "|") || len(got) != len(want) {
			t.Errorf("SplitArgs(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{`'open`, `"open`} {
		if _, err := SplitArgs(bad); err == nil {
			t.Errorf("SplitArgs(%q) should fail", bad)
		}
	}
}
