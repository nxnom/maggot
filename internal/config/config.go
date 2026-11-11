// Package config stores user-level maggot settings (registered toolboxes).
// Nothing here lives inside a toolbox, so toolboxes stay safe to commit.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

const fileName = "config.json"

var namePattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

type ToolboxRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Config struct {
	Toolboxes []ToolboxRef `json:"toolboxes"`
	Default   string       `json:"default,omitempty"`

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
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return c, nil
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
