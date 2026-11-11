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

// LangBin makes a manifest-only tool that runs a program on PATH.
const LangBin = "bin"

// Langs lists the template kinds.
func Langs() []string {
	out := []string{LangBin}
	for k := range langs {
		out = append(out, k)
	}
	sort.Strings(out[1:])
	return out
}

type Options struct {
	ID          string
	Lang        string
	Name        string
	Description string
	// Bin is the program to run, for Lang "bin".
	Bin string
	// Warm adds a serve mode that keeps the tool loaded (python only).
	Warm bool
}

// Create writes a new tool folder at parent/<id> and returns its path.
func Create(parent string, o Options) (string, error) {
	if o.Name == "" {
		o.Name = titleCase(o.ID)
	}
	if o.Lang == LangBin {
		return createBin(parent, o)
	}
	l, ok := langs[o.Lang]
	if !ok {
		return "", fmt.Errorf("unknown kind %q (choose: %s)", o.Lang, strings.Join(Langs(), ", "))
	}
	if o.Warm && o.Lang != "python" {
		return "", fmt.Errorf("--warm is available for python tools (see `maggot guide` for the protocol to do it in %s)", o.Lang)
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
		Outputs:  &manifest.Outputs{Kind: manifest.OutputFiles, DirFlag: "--out"},
		Examples: []string{fmt.Sprintf("maggot run %s -- --upper --out out notes.txt", o.ID)},
	}
	tmpl := o.Lang
	if o.Warm {
		tmpl = "python-warm"
		m.Warm = &manifest.Warm{Args: []string{"--serve"}}
	}
	if err := m.Validate(); err != nil {
		return "", err
	}

	dir, err := newDir(parent, o.ID)
	if err != nil {
		return "", err
	}
	src, err := templates.ReadFile("templates/" + tmpl + "/" + l.file)
	if err != nil {
		return "", err
	}
	body := strings.NewReplacer("{{ID}}", o.ID, "{{NAME}}", o.Name, "{{DESCRIPTION}}", o.Description).Replace(string(src))
	extra := []file{}
	if o.Warm {
		helper, err := templates.ReadFile("templates/python-warm/maggot_warm.py")
		if err != nil {
			return "", err
		}
		extra = append(extra, file{"maggot_warm.py", string(helper), 0o644})
	}
	return write(dir, m, append(extra, []file{
		{l.file, body, 0o755},
		{"tests/fixtures/hello.txt", "hello maggot\n", 0o644},
		{"tests/cases.json", casesJSON(), 0o644},
		{"README.md", readme(m, "./"+l.file, "--upper --out out notes.txt"), 0o644},
	}...))
}

type file struct {
	path string
	data string
	mode os.FileMode
}

func newDir(parent, id string) (string, error) {
	dir := filepath.Join(parent, id)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("%s already exists", dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return dir, nil
}

// write saves tool.json plus files and checks the result loads.
func write(dir string, m *manifest.Manifest, files []file) (string, error) {
	manifestJSON, _ := json.MarshalIndent(m, "", "  ")
	files = append([]file{{manifest.FileName, string(manifestJSON) + "\n", 0o644}}, files...)
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

// createBin makes a manifest-only tool for a program on PATH.
func createBin(parent string, o Options) (string, error) {
	if o.Bin == "" {
		return "", fmt.Errorf("--bin is required for kind bin (the program to run, e.g. yt-dlp)")
	}
	if o.Description == "" {
		o.Description = fmt.Sprintf("Run %s. (Template: replace with what the tool does.)", o.Bin)
	}
	m := &manifest.Manifest{
		Schema: manifest.SchemaVersion, ID: o.ID, Version: "0.1.0", Name: o.Name, Description: o.Description,
		Tags:     []string{o.Bin},
		Entry:    manifest.Entry{Bin: o.Bin},
		Requires: []manifest.Requirement{{Bin: o.Bin, Install: map[string]string{"brew": o.Bin}}},
		Inputs: []manifest.Input{
			{Name: "args", Label: "Arguments", Type: manifest.TypeArgs, Placeholder: "--help"},
		},
		Outputs:  &manifest.Outputs{Kind: manifest.OutputStdout},
		Examples: []string{fmt.Sprintf("maggot run %s -- --help", o.ID)},
	}
	if err := m.Validate(); err != nil {
		return "", err
	}
	dir, err := newDir(parent, o.ID)
	if err != nil {
		return "", err
	}
	cases := fmt.Sprintf(`{
  "cases": [
    {
      "name": "%s runs",
      "args": ["--version"],
      "expect": {"exit": 0}
    }
  ]
}
`, o.Bin)
	return write(dir, m, []file{
		{"tests/cases.json", cases, 0o644},
		{"README.md", readme(m, o.Bin, "--help"), 0o644},
	})
}

func readme(m *manifest.Manifest, entry, args string) string {
	return fmt.Sprintf(`# %s

%s

## Usage

    %s %s
    maggot run %s -- %s

## Notes

Write anything an agent or person needs to know here: limits, input formats,
side effects, examples.
`, m.Name, m.Description, entry, args, m.ID, args)
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
