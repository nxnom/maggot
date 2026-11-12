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
	// OneOf lists groups of inputs where at least one must have a value,
	// e.g. [["text", "lines"]]: give text, or a lines file, or both.
	OneOf   [][]string `json:"one_of,omitempty"`
	Outputs *Outputs   `json:"outputs,omitempty"`
	// Warm says the tool can stay loaded in memory between runs.
	Warm     *Warm    `json:"warm,omitempty"`
	Examples []string `json:"examples,omitempty"`
}

// Entry is the program to start. Use exactly one of Path or Bin.
//
// Path is a file in the tool folder. With Interpreter, maggot runs
// "interpreter... <abs path> args..."; otherwise Path must be executable.
//
// Bin is a program on PATH (for example "yt-dlp"). The tool folder then holds
// only the manifest, README and tests: a manifest-only tool.
//
// Args are fixed arguments placed before the user's arguments.
type Entry struct {
	Path        string   `json:"path,omitempty"`
	Interpreter []string `json:"interpreter,omitempty"`
	Bin         string   `json:"bin,omitempty"`
	Args        []string `json:"args,omitempty"`
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
	// TypeStrings is a list of text values (for example URLs).
	TypeStrings InputType = "strings"
	// TypeArgs is free text split like a shell command line and passed as
	// extra args. It needs no arg mapping.
	TypeArgs InputType = "args"
)

var inputTypes = map[InputType]bool{
	TypeString: true, TypeText: true, TypeInteger: true, TypeNumber: true, TypeBoolean: true,
	TypeEnum: true, TypeFile: true, TypeFiles: true, TypeDirectory: true, TypeStrings: true, TypeArgs: true,
}

// IsList reports whether the input takes a list of values.
func (t InputType) IsList() bool { return t == TypeFiles || t == TypeStrings }

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
	Arg         Arg       `json:"arg,omitzero"`

	// Form hints for the web UI. They do not change argv.
	Placeholder string `json:"placeholder,omitempty"`
	Group       string `json:"group,omitempty"`
	Advanced    bool   `json:"advanced,omitempty"`
}

// ChoiceArgs reports whether this enum maps each option to its own args
// instead of using Arg.
func (in Input) ChoiceArgs() bool {
	return in.Type == TypeEnum && len(in.Options) > 0 && in.Options[0].Args != nil
}

// Arg says how an input becomes argv: a flag ("--width 10"), a boolean flag
// ("--force" only when true) or a positional argument.
type Arg struct {
	Flag       string `json:"flag,omitempty"`
	Positional bool   `json:"positional,omitempty"`
	// Repeat makes a list input with a flag emit the flag for every value.
	Repeat bool `json:"repeat,omitempty"`
	// Args, for a boolean, are emitted when true (instead of Flag).
	Args []string `json:"args,omitempty"`
	// Template formats the value, e.g. "res:{value}" gives "-S res:1080".
	Template string `json:"template,omitempty"`
}

// Option is one choice of an enum input. In JSON it can be a plain string or
// {"value": "...", "label": "...", "args": [...]}. When options have Args,
// choosing one emits those args (an empty list emits nothing).
type Option struct {
	Value string   `json:"value"`
	Label string   `json:"label,omitempty"`
	Args  []string `json:"args"`
}

// MarshalJSON keeps "args": [] (meaning "emit nothing") but drops nil args.
func (o Option) MarshalJSON() ([]byte, error) {
	type withArgs struct {
		Value string   `json:"value"`
		Label string   `json:"label,omitempty"`
		Args  []string `json:"args"`
	}
	type noArgs struct {
		Value string `json:"value"`
		Label string `json:"label,omitempty"`
	}
	if o.Args != nil {
		return json.Marshal(withArgs(o))
	}
	return json.Marshal(noArgs{o.Value, o.Label})
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

// Warm describes the tool's serve mode. `maggot warm` starts the entry with
// Args (for example ["--serve"]); the tool loads once, listens on the socket
// in $MAGGOT_WARM_SOCKET and exits after $MAGGOT_WARM_IDLE seconds without
// jobs. Normal runs forward their job to it when it is running.
type Warm struct {
	Args []string `json:"args"`
}

// Output kinds.
const (
	OutputFiles   = "files"   // writes new files (into DirFlag when set)
	OutputStdout  = "stdout"  // the result is what it prints
	OutputInplace = "inplace" // changes the input files
	OutputNone    = "none"    // side effects only (send mail, call an API)
)

// Outputs tells maggot and the UI what kind of result the tool produces.
// It is a description, not a restriction: the tool decides where it writes.
type Outputs struct {
	Kind string `json:"kind"`
	// DirFlag is the flag that takes the output folder (kind "files"), e.g. "--out".
	DirFlag string `json:"dir_flag,omitempty"`
	// Format of stdout for kind "stdout": "text" (default) or "json".
	Format string `json:"format,omitempty"`
	// Progress reads progress from output lines, for tools that do not
	// write MAGGOT_EVENTS (for example yt-dlp).
	Progress *Progress `json:"progress,omitempty"`
}

// Progress is a regex whose first group is a number on stdout or stderr
// lines. Max is the value that means done (default 100, i.e. percent).
type Progress struct {
	Regex string  `json:"regex"`
	Max   float64 `json:"max,omitempty"`
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
// interpreter is set, that it is executable. Bin entries have no file.
func (m *Manifest) CheckFiles(dir string) error {
	if m.Entry.Bin != "" {
		return nil
	}
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

// EntryPath returns the absolute path of the entry file for a tool in dir,
// or the bin name for a bin entry.
func (m *Manifest) EntryPath(dir string) (string, error) {
	if m.Entry.Bin != "" {
		return m.Entry.Bin, nil
	}
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
