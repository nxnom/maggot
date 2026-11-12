package agentskill

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sample = `---
name: video-short
description: "Make a short video: stills, voices, captions. Use when asked for a short."
license: MIT
metadata:
  version: 1.2.0
  maggot-uses: voxcpm-tts, depth-map
  maggot-requires: ffmpeg node
  maggot-env: STABILITY_API_KEY
  author: someone
---
# Video short

1. Write the script.
`

func TestParse(t *testing.T) {
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if s.Meta.Name != "video-short" || s.Version != "1.2.0" || s.Meta.License != "MIT" {
		t.Fatalf("meta: %+v", s)
	}
	if !reflect.DeepEqual(s.Uses, []string{"voxcpm-tts", "depth-map"}) || !reflect.DeepEqual(s.Requires, []string{"ffmpeg", "node"}) ||
		!reflect.DeepEqual(s.Env, []string{"STABILITY_API_KEY"}) {
		t.Fatalf("lists: %q %q %q", s.Uses, s.Requires, s.Env)
	}
	if !strings.HasPrefix(s.Body, "# Video short\n") {
		t.Fatalf("body: %q", s.Body)
	}
}

func TestParseCRLFAndYAMLLists(t *testing.T) {
	text := "---\r\nname: a-b\r\ndescription: x\r\nmetadata:\r\n  maggot-uses: [one, two]\r\n---\r\nbody\r\n"
	s, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Uses, []string{"one", "two"}) || s.Body != "body\n" || s.Version != "" {
		t.Fatalf("got %+v", s)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no frontmatter":  "# hello\n",
		"not closed":      "---\nname: a\n",
		"bad name":        "---\nname: Video_Short\ndescription: x\n---\n",
		"double hyphen":   "---\nname: a--b\ndescription: x\n---\n",
		"no description":  "---\nname: a\n---\n",
		"reserved":        "---\nname: maggot\ndescription: x\n---\n",
		"bad env":         "---\nname: a\ndescription: x\nmetadata:\n  maggot-env: NOT-OK\n---\n",
		"long name":       "---\nname: " + strings.Repeat("a", 65) + "\ndescription: x\n---\n",
		"bad yaml":        "---\nname: [\n---\n",
		"long descriptio": "---\nname: a\ndescription: " + strings.Repeat("x", 1025) + "\n---\n",
	}
	for name, text := range cases {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestLoadNamedChecksFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "other")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, FileName), []byte(sample), 0o644)
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNamed(dir); err == nil || !strings.Contains(err.Error(), "must match") {
		t.Fatalf("got %v", err)
	}
}

func TestTemplateParses(t *testing.T) {
	s, err := Parse([]byte(Template("my-skill", "Does things: well. Use when needed.")))
	if err != nil {
		t.Fatal(err)
	}
	if s.Meta.Description != "Does things: well. Use when needed." || s.Version != "0.1.0" || len(s.Uses) != 0 {
		t.Fatalf("got %+v", s)
	}
	if _, err := Parse([]byte(Template("my-skill", ""))); err != nil {
		t.Fatal(err)
	}
}
