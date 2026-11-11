// Package scaffold creates a new tool folder from a working template.
package scaffold

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nxnom/maggot/internal/manifest"
)

//go:embed templates
var templates embed.FS

type lang struct {
	file        string
	interpreter []string
	requires    []manifest.Requirement
}

var langs = map[string]lang{
	"python": {"run.py", []string{"uv", "run", "--script"},
		[]manifest.Requirement{{Bin: "uv", Install: map[string]string{"brew": "uv", "winget": "astral-sh.uv", "pipx": "uv"}}}},
	"sh": {"run.sh", []string{"sh"}, nil},
	"deno": {"run.ts", []string{"deno", "run", "--allow-read", "--allow-write", "--allow-env"},
		[]manifest.Requirement{{Bin: "deno", Install: map[string]string{"brew": "deno", "winget": "DenoLand.Deno", "npm": "deno"}}}},
}

// Langs lists the template languages.
func Langs() []string {
	out := make([]string, 0, len(langs))
	for k := range langs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type Options struct {
	ID          string
	Lang        string
	Name        string
	Description string
}

// Create writes a new tool folder at parent/<id> and returns its path.
func Create(parent string, o Options) (string, error) {
	l, ok := langs[o.Lang]
	if !ok {
		return "", fmt.Errorf("unknown language %q (choose: %s)", o.Lang, strings.Join(Langs(), ", "))
	}
	if o.Name == "" {
		o.Name = titleCase(o.ID)
	}
	if o.Description == "" {
		o.Description = "Copy text files, optionally in upper case. (Template: replace with what the tool does.)"
	}
	m := &manifest.Manifest{
		Schema: manifest.SchemaVersion, ID: o.ID, Version: "0.1.0", Name: o.Name, Description: o.Description,
		Categories: []string{"Files"}, Tags: []string{"template"},
		Entry:    manifest.Entry{Path: l.file, Interpreter: l.interpreter},
		Requires: l.requires,
		Inputs: []manifest.Input{
			{Name: "upper", Label: "Upper case", Description: "Convert text to upper case", Type: manifest.TypeBoolean, Arg: manifest.Arg{Flag: "--upper"}},
			{Name: "files", Label: "Text files", Type: manifest.TypeFiles, Required: true, Accept: []string{"text/*"}, Arg: manifest.Arg{Positional: true}},
		},
		Outputs:  &manifest.Outputs{DirFlag: "--out", Preview: "file"},
		Examples: []string{fmt.Sprintf("maggot run %s -- --upper --out out notes.txt", o.ID)},
	}
	if err := m.Validate(); err != nil {
		return "", err
	}

	dir := filepath.Join(parent, o.ID)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("%s already exists", dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	src, err := templates.ReadFile("templates/" + o.Lang + "/" + l.file)
	if err != nil {
		return "", err
	}
	body := strings.NewReplacer("{{ID}}", o.ID, "{{NAME}}", o.Name, "{{DESCRIPTION}}", o.Description).Replace(string(src))
	manifestJSON, _ := json.MarshalIndent(m, "", "  ")

	files := []struct {
		path string
		data string
		mode os.FileMode
	}{
		{manifest.FileName, string(manifestJSON) + "\n", 0o644},
		{l.file, body, 0o755},
		{"tests/fixtures/hello.txt", "hello maggot\n", 0o644},
		{"tests/cases.json", casesJSON(), 0o644},
		{"README.md", readme(m, l.file), 0o644},
	}
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, []byte(f.data), f.mode); err != nil {
			return "", err
		}
	}
	if _, err := manifest.Load(dir); err != nil {
		return "", fmt.Errorf("scaffold produced an invalid tool (bug): %w", err)
	}
	return dir, nil
}

func casesJSON() string {
	return `{
  "cases": [
    {
      "name": "copies in upper case",
      "args": ["--upper", "--out", "{work}/out", "{tool}/tests/fixtures/hello.txt"],
      "expect": {"stdout_contains": ["hello.out.txt"], "files": ["out/hello.out.txt"]}
    },
    {
      "name": "same through inputs (the web UI path)",
      "inputs": {"upper": true, "files": ["{tool}/tests/fixtures/hello.txt"]},
      "out": "{work}/out",
      "expect": {"files": ["out/hello.out.txt"]}
    },
    {
      "name": "missing file fails",
      "args": ["--out", "{work}", "{work}/nope.txt"],
      "expect": {"exit": 2, "stderr_contains": ["not a file"]}
    }
  ]
}
`
}

func readme(m *manifest.Manifest, entry string) string {
	return fmt.Sprintf(`# %s

%s

## Usage

    ./%s --upper --out out notes.txt
    maggot run %s -- --upper --out out notes.txt

## Notes

Write anything an agent or person needs to know here: limits, input formats,
side effects, examples.
`, m.Name, m.Description, entry, m.ID)
}

func titleCase(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
