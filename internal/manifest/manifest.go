// Package manifest defines tool.json: the file that describes one standalone tool.
//
// The manifest is the only thing maggot (and the web UI) needs to understand a
// tool. The tool itself stays a plain CLI program that receives normal argv.
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// FileName is the manifest file name inside a tool folder.
const FileName = "tool.json"

// SchemaVersion is the manifest schema this build understands.
const SchemaVersion = 1

type Manifest struct {
	Schema      int      `json:"schema"`
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Categories  []string `json:"categories,omitempty"`
	// Tags are extra search words (synonyms, formats, library names).
	Tags     []string      `json:"tags,omitempty"`
	Icon     string        `json:"icon,omitempty"`
	Entry    Entry         `json:"entry"`
	Requires []Requirement `json:"requires,omitempty"`
	Env      []EnvVar      `json:"env,omitempty"`
	Inputs   []Input       `json:"inputs,omitempty"`
	Outputs  *Outputs      `json:"outputs,omitempty"`
	Examples []string      `json:"examples,omitempty"`
}

// Entry is the program to start. Path is relative to the tool folder.
// When Interpreter is set, maggot runs: interpreter... <abs path> args...
// Otherwise Path must be an executable file (for example a script with a shebang).
type Entry struct {
	Path        string   `json:"path"`
	Interpreter []string `json:"interpreter,omitempty"`
}

// Requirement is an external binary the tool needs on PATH.
type Requirement struct {
	Bin string `json:"bin"`
	// Version is a semver constraint such as ">=7" or "^1.2". Optional.
	Version string `json:"version,omitempty"`
	// Check are the args used to print the version. Default: ["--version"].
	Check []string `json:"check,omitempty"`
	// Install maps a package manager (brew, apt, winget, ...) to a package name.
	Install map[string]string `json:"install,omitempty"`
}

// EnvVar is an environment variable the tool reads. Secret values are stored
// by maggot outside the toolbox and injected at run time.
type EnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type InputType string

const (
	TypeString    InputType = "string"
	TypeText      InputType = "text"
	TypeInteger   InputType = "integer"
	TypeNumber    InputType = "number"
	TypeBoolean   InputType = "boolean"
	TypeEnum      InputType = "enum"
	TypeFile      InputType = "file"
	TypeFiles     InputType = "files"
	TypeDirectory InputType = "directory"
)

var inputTypes = map[InputType]bool{
	TypeString: true, TypeText: true, TypeInteger: true, TypeNumber: true, TypeBoolean: true,
	TypeEnum: true, TypeFile: true, TypeFiles: true, TypeDirectory: true,
}

// Input describes one argument. The UI renders a widget from it and maggot
// turns the value into argv using Arg.
type Input struct {
	Name        string    `json:"name"`
	Label       string    `json:"label,omitempty"`
	Description string    `json:"description,omitempty"`
	Type        InputType `json:"type"`
	Required    bool      `json:"required,omitempty"`
	Default     any       `json:"default,omitempty"`
	Min         *float64  `json:"min,omitempty"`
	Max         *float64  `json:"max,omitempty"`
	Options     []Option  `json:"options,omitempty"`
	Accept      []string  `json:"accept,omitempty"`
	Arg         Arg       `json:"arg"`
}

// Arg says how an input becomes argv: a flag ("--width 10"), a boolean flag
// ("--force" only when true) or a positional argument.
type Arg struct {
	Flag       string `json:"flag,omitempty"`
	Positional bool   `json:"positional,omitempty"`
	// Repeat makes a "files" input with a flag emit the flag for every file.
	Repeat bool `json:"repeat,omitempty"`
}

// Option is one choice of an enum input. In JSON it can be a plain string or
// {"value": "...", "label": "..."}.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

func (o *Option) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*o = Option{Value: s}
		return nil
	}
	type plain Option
	var p plain
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("option must be a string or {value, label}: %w", err)
	}
	*o = Option(p)
	return nil
}

// Outputs tells maggot and the UI how the tool writes results.
type Outputs struct {
	// DirFlag is the flag that receives the output directory, e.g. "--out".
	DirFlag string `json:"dir_flag,omitempty"`
	// Preview hints how the UI shows results: none, file, image, text, json.
	Preview string `json:"preview,omitempty"`
}

// Parse decodes a manifest strictly (unknown fields are errors) and validates it.
func Parse(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", FileName, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("invalid %s: trailing data after JSON object", FileName)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Load reads and validates <dir>/tool.json and checks that the entry file exists.
func Load(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	m, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := m.CheckFiles(dir); err != nil {
		return nil, err
	}
	return m, nil
}

// CheckFiles verifies the entry file exists inside dir and, when no
// interpreter is set, that it is executable.
func (m *Manifest) CheckFiles(dir string) error {
	p := filepath.Join(dir, filepath.FromSlash(m.Entry.Path))
	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("entry.path: %q does not exist", m.Entry.Path)
	}
	if err != nil {
		return fmt.Errorf("entry.path: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("entry.path: %q is a directory", m.Entry.Path)
	}
	if len(m.Entry.Interpreter) == 0 && runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return fmt.Errorf("entry.path: %q is not executable (chmod +x it or set entry.interpreter)", m.Entry.Path)
	}
	return nil
}

// EntryPath returns the absolute path of the entry file for a tool in dir.
func (m *Manifest) EntryPath(dir string) (string, error) {
	abs, err := filepath.Abs(filepath.Join(dir, filepath.FromSlash(m.Entry.Path)))
	if err != nil {
		return "", err
	}
	return abs, nil
}

// Title returns the label shown for an input.
func (in Input) Title() string {
	if in.Label != "" {
		return in.Label
	}
	return strings.ReplaceAll(in.Name, "_", " ")
}
