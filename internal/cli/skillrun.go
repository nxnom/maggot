package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// skillFile returns the absolute path of rel inside the skill folder dir. It
// rejects paths that leave the folder and files that do not exist.
func skillFile(dir, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("give a path inside the skill folder, not an absolute path: %s", rel)
	}
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if r, err := filepath.Rel(dir, p); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the skill folder", rel)
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return "", fmt.Errorf("no such file in the skill folder: %s", rel)
	}
	return p, nil
}

// skillScriptCommand finds a script (in scripts/ first, then relative to the
// skill folder) and returns the argv that runs it: interpreter + script path.
func skillScriptCommand(dir, script string, lookPath func(string) (string, error)) ([]string, error) {
	p, err := skillFile(dir, filepath.ToSlash(filepath.Join("scripts", script)))
	if err != nil {
		var err2 error
		if p, err2 = skillFile(dir, script); err2 != nil {
			return nil, fmt.Errorf("script %q not found in %s (looked in scripts/ and the skill folder)", script, dir)
		}
	}
	has := func(bin string) bool {
		if lookPath != nil {
			_, err := lookPath(bin)
			return err == nil
		}
		return true
	}
	need := func(bin string) error {
		if !has(bin) {
			return fmt.Errorf("%s is not on PATH (needed to run %s)", bin, filepath.Base(p))
		}
		return nil
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".py":
		if hasPEP723(p) {
			if err := need("uv"); err != nil {
				return nil, err
			}
			return []string{"uv", "run", "--script", p}, nil
		}
		py := "python3"
		if runtime.GOOS == "windows" {
			py = "python"
		}
		if err := need(py); err != nil {
			return nil, err
		}
		return []string{py, p}, nil
	case ".sh":
		if err := need("sh"); err != nil {
			return nil, err
		}
		return []string{"sh", p}, nil
	case ".js", ".mjs":
		if err := need("node"); err != nil {
			return nil, err
		}
		return []string{"node", p}, nil
	case ".ts":
		if err := need("deno"); err != nil {
			return nil, err
		}
		return []string{"deno", "run", "-A", p}, nil
	}
	if st, err := os.Stat(p); err == nil && st.Mode()&0o111 != 0 {
		return []string{p}, nil
	}
	return nil, fmt.Errorf("don't know how to run %s: use .py, .sh, .js, .ts or make it executable", filepath.Base(p))
}

// hasPEP723 reports whether a Python file starts with an inline script block.
func hasPEP723(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(b, []byte("\n# /// script")) || bytes.HasPrefix(b, []byte("# /// script"))
}

// hasScripts reports whether the file list has anything under scripts/.
func hasScripts(files []string) bool {
	for _, f := range files {
		if strings.HasPrefix(filepath.ToSlash(f), "scripts/") {
			return true
		}
	}
	return false
}
