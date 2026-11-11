package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigCommands(t *testing.T) {
	isolate(t)
	cfgDir := os.Getenv("MAGGOT_CONFIG_DIR")

	if code, out, _ := run(t, "config", "path"); code != 0 || strings.TrimSpace(out) != filepath.Join(cfgDir, "config.json") {
		t.Fatalf("path: %q", out)
	}
	newToolbox(t, "main")
	if code, out, _ := run(t, "config", "show"); code != 0 || !strings.Contains(out, `"default_toolbox": "main"`) {
		t.Fatalf("show: %s", out)
	}
	if code, out, _ := run(t, "config", "validate"); code != 0 || !strings.Contains(out, "config ok") {
		t.Fatalf("validate: %s", out)
	}
	os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"toolboxes":[{"name":"x","path":"/nope"}]}`), 0o644)
	if code, out, _ := run(t, "config", "validate"); code != 1 || !strings.Contains(out, "no maggot.json") {
		t.Fatalf("invalid: %s", out)
	}
}

func TestConfigEditUsesEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell editor")
	}
	isolate(t)
	// A fake "editor" that writes an empty valid config.
	editor := filepath.Join(t.TempDir(), "ed")
	os.WriteFile(editor, []byte("#!/bin/sh\necho '{\"toolboxes\":[]}' > \"$1\"\n"), 0o755)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editor)
	if code, out, e := run(t, "config"); code != 0 || !strings.Contains(out, "config ok") {
		t.Fatalf("edit: code=%d %s %s", code, out, e)
	}
	t.Setenv("VISUAL", "definitely-not-an-editor-xyz --wait")
	if code, _, e := run(t, "config", "edit"); code == 0 || !strings.Contains(e, "definitely-not-an-editor-xyz") {
		t.Fatalf("bad editor: %s", e)
	}
}
