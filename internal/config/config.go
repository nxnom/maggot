// Package config stores user-level maggot settings (registered toolboxes).
// Nothing here lives inside a toolbox, so toolboxes stay safe to commit.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const fileName = "config.json"

var namePattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

type ToolboxRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Config struct {
	Toolboxes []ToolboxRef `json:"toolboxes"`
	Default   string       `json:"default_toolbox,omitempty"`
	// RunsKeep is how many web UI runs to keep per tool (default 30).
	RunsKeep int `json:"runs_keep,omitempty"`
	// Editor opens tool folders, e.g. "code" or "cursor -n". Empty: auto-detect.
	Editor string `json:"editor,omitempty"`

	path string
}

// Dir returns the maggot config directory:
// $MAGGOT_CONFIG_DIR, else $XDG_CONFIG_HOME/maggot, else ~/.config/maggot
// (%AppData%\maggot on Windows).
func Dir() (string, error) {
	if d := os.Getenv("MAGGOT_CONFIG_DIR"); d != "" {
		return filepath.Abs(d)
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "maggot"), nil
	}
	if runtime.GOOS == "windows" {
		d, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(d, "maggot"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "maggot"), nil
}

// DefaultRunsKeep is used when runs_keep is not set.
const DefaultRunsKeep = 30

// Keep returns how many runs to keep per tool.
func (c *Config) Keep() int {
	if c.RunsKeep > 0 {
		return c.RunsKeep
	}
	return DefaultRunsKeep
}

// DataDir is where maggot keeps data it creates (web UI runs, uploads):
// $MAGGOT_DATA_DIR, else $XDG_DATA_HOME/maggot, else ~/.local/share/maggot
// (%LocalAppData%\maggot on Windows).
func DataDir() (string, error) {
	if d := os.Getenv("MAGGOT_DATA_DIR"); d != "" {
		return filepath.Abs(d)
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "maggot"), nil
	}
	if runtime.GOOS == "windows" {
		d, err := os.UserCacheDir() // %LocalAppData%
		if err != nil {
			return "", err
		}
		return filepath.Join(d, "maggot"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "maggot"), nil
}

// DraftsDir is where `maggot new` creates tools by default: a lasting place
// outside every toolbox, so the user can try a tool before importing it.
func DraftsDir() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "drafts"), nil
}

// Load reads the config file. A missing file gives an empty config.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return LoadFile(filepath.Join(dir, fileName))
}

func LoadFile(path string) (*Config, error) {
	c := &Config{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(c); err != nil {
		return nil, fmt.Errorf("%s: %w (fix it with `maggot config`)", path, err)
	}
	for i := range c.Toolboxes {
		c.Toolboxes[i].Path = ExpandHome(c.Toolboxes[i].Path)
	}
	return c, nil
}

// ExpandHome turns a leading "~" into the home directory.
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

// Problems checks the config for mistakes a person may make when editing it
// by hand. Missing toolbox folders are reported too.
func (c *Config) Problems() []string {
	var out []string
	names := map[string]bool{}
	paths := map[string]bool{}
	for i, t := range c.Toolboxes {
		f := fmt.Sprintf("toolboxes[%d]", i)
		if !ValidName(t.Name) {
			out = append(out, fmt.Sprintf("%s.name: invalid %q (use lowercase letters, digits, - or _)", f, t.Name))
		}
		if names[t.Name] {
			out = append(out, fmt.Sprintf("%s.name: duplicate %q", f, t.Name))
		}
		names[t.Name] = true
		switch {
		case t.Path == "":
			out = append(out, f+".path: is required")
		case !filepath.IsAbs(t.Path):
			out = append(out, fmt.Sprintf("%s.path: must be absolute or start with ~ (got %q)", f, t.Path))
		default:
			if paths[t.Path] {
				out = append(out, fmt.Sprintf("%s.path: duplicate %q", f, t.Path))
			}
			paths[t.Path] = true
			if _, err := os.Stat(filepath.Join(t.Path, "maggot.json")); err != nil {
				out = append(out, fmt.Sprintf("%s.path: %s has no maggot.json", f, t.Path))
			}
		}
	}
	if c.RunsKeep < 0 {
		out = append(out, "runs_keep: must be 0 (default) or more")
	}
	if c.Default != "" && !names[c.Default] {
		out = append(out, fmt.Sprintf("default_toolbox: %q is not a registered toolbox", c.Default))
	}
	return out
}

// Path is where the config is saved.
func (c *Config) Path() string { return c.path }

// Save writes the config atomically.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(c.path, append(data, '\n'), 0o644)
}

// WriteFileAtomic writes to a temp file in the same folder and renames it.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ValidName reports whether name can be used as a toolbox handle.
func ValidName(name string) bool { return namePattern.MatchString(name) && len(name) <= 40 }

// Find returns the toolbox registered under name.
func (c *Config) Find(name string) (ToolboxRef, bool) {
	for _, t := range c.Toolboxes {
		if t.Name == name {
			return t, true
		}
	}
	return ToolboxRef{}, false
}

// Add registers a toolbox. The first toolbox becomes the default.
func (c *Config) Add(name, path string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid toolbox name %q (use lowercase letters, digits, - or _)", name)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, t := range c.Toolboxes {
		if t.Name == name {
			return fmt.Errorf("a toolbox named %q is already registered (%s)", name, t.Path)
		}
		if t.Path == abs {
			return fmt.Errorf("%s is already registered as %q", abs, t.Name)
		}
	}
	c.Toolboxes = append(c.Toolboxes, ToolboxRef{Name: name, Path: abs})
	if c.Default == "" {
		c.Default = name
	}
	return nil
}

// Remove unregisters a toolbox. Files on disk are not touched.
func (c *Config) Remove(name string) error {
	for i, t := range c.Toolboxes {
		if t.Name == name {
			c.Toolboxes = append(c.Toolboxes[:i], c.Toolboxes[i+1:]...)
			if c.Default == name {
				c.Default = ""
				if len(c.Toolboxes) > 0 {
					c.Default = c.Toolboxes[0].Name
				}
			}
			return nil
		}
	}
	return fmt.Errorf("no toolbox named %q", name)
}

// SetDefault marks a registered toolbox as the default target for imports.
func (c *Config) SetDefault(name string) error {
	if _, ok := c.Find(name); !ok {
		return fmt.Errorf("no toolbox named %q", name)
	}
	c.Default = name
	return nil
}
