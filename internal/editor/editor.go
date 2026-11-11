// Package editor opens a folder in the user's code editor.
package editor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Editor is a way to open a folder.
type Editor struct {
	Name string
	Argv []string // the folder path is appended
}

// candidates are tried in order when the config has no "editor".
var candidates = []struct{ name, bin, macApp string }{
	{"Cursor", "cursor", "Cursor"},
	{"VS Code", "code", "Visual Studio Code"},
	{"Zed", "zed", "Zed"},
	{"Sublime Text", "subl", "Sublime Text"},
	{"Windsurf", "windsurf", "Windsurf"},
}

// Find returns the configured editor (a command line such as "code -n"),
// else the first known editor found on PATH or, on macOS, in /Applications.
// $EDITOR is not used: it is usually a terminal editor, which cannot be
// opened from the web UI.
func Find(configured string, lookPath func(string) (string, error)) (Editor, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if f := strings.Fields(configured); len(f) > 0 {
		return Editor{Name: filepath.Base(f[0]), Argv: f}, nil
	}
	for _, c := range candidates {
		if _, err := lookPath(c.bin); err == nil {
			return Editor{Name: c.name, Argv: []string{c.bin}}, nil
		}
	}
	if runtime.GOOS == "darwin" {
		for _, c := range candidates {
			if _, err := os.Stat("/Applications/" + c.macApp + ".app"); err == nil {
				return Editor{Name: c.name, Argv: []string{"open", "-a", c.macApp}}, nil
			}
		}
	}
	return Editor{}, errors.New(`no code editor found; set "editor" in maggot's config.json (for example "code" or "cursor")`)
}

// Open starts the editor on dir without waiting for it.
func (e Editor) Open(dir string) error {
	argv := append(append([]string{}, e.Argv...), dir)
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
