package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// isolate gives each test its own config dir and a neutral working directory.
func isolate(t *testing.T) string {
	t.Helper()
	t.Setenv("MAGGOT_CONFIG_DIR", t.TempDir())
	t.Setenv("MAGGOT_TOOLBOX", "")
	t.Chdir(t.TempDir())
	return t.TempDir()
}

// addTool writes a tool folder with a shell entry into toolboxDir/tools/<id>.
func addTool(t *testing.T, toolboxDir, id, manifestJSON, script string) string {
	t.Helper()
	dir := filepath.Join(toolboxDir, "tools", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

const helloManifest = `{
  "schema": 1, "id": "hello", "version": "1.0.0", "name": "Hello",
  "description": "Print a greeting.", "categories": ["Demo"],
  "entry": {"path": "run"},
  "inputs": [
    {"name": "name", "type": "string", "required": true, "arg": {"positional": true}},
    {"name": "shout", "type": "boolean", "arg": {"flag": "--shout"}}
  ],
  "examples": ["run world"]
}`

const helloScript = `#!/bin/sh
if [ "$1" = "--shout" ]; then shift; echo "HELLO $1"; else echo "hello $1"; fi
`

// newToolbox runs `maggot toolbox init` and returns the toolbox path.
func newToolbox(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if code, _, errOut := run(t, "toolbox", "init", dir, "--name", name); code != 0 {
		t.Fatalf("toolbox init: %s", errOut)
	}
	return dir
}
