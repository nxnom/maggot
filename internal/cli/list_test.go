package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolboxInitListRemove(t *testing.T) {
	isolate(t)
	dir := newToolbox(t, "personal")
	if _, err := os.Stat(filepath.Join(dir, "maggot.json")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "toolbox", "list", "--json")
	var got struct {
		Toolboxes []struct {
			Name    string
			Default bool
			OK      bool
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || len(got.Toolboxes) != 1 || !got.Toolboxes[0].Default {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if code, _, _ := run(t, "toolbox", "remove", "personal"); code != 0 {
		t.Fatal("remove failed")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("remove must not delete files")
	}
}

func TestToolboxAddExisting(t *testing.T) {
	isolate(t)
	dir := filepath.Join(t.TempDir(), "team")
	if code, _, e := run(t, "toolbox", "init", dir, "--no-register"); code != 0 {
		t.Fatal(e)
	}
	code, out, _ := run(t, "toolbox", "add", dir)
	if code != 0 || !strings.Contains(out, `"team"`) {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if code, _, e := run(t, "toolbox", "add", t.TempDir()); code == 0 || !strings.Contains(e, "not a toolbox") {
		t.Fatalf("adding a plain folder should fail: %s", e)
	}
}

func TestListAndInfo(t *testing.T) {
	isolate(t)
	dir := newToolbox(t, "personal")
	addTool(t, dir, "hello", helloManifest, helloScript)
	addTool(t, dir, "broken", `{"schema":1}`, "")

	code, out, errOut := run(t, "list")
	if code != 0 || !strings.Contains(out, "personal/hello") || !strings.Contains(errOut, "warning:") {
		t.Fatalf("code=%d out=%s err=%s", code, out, errOut)
	}

	code, out, _ = run(t, "list", "--json")
	var listed struct {
		Tools    []toolSummary
		Problems []struct{ Path string }
	}
	if code != 0 || json.Unmarshal([]byte(out), &listed) != nil {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Ref != "personal/hello" || len(listed.Problems) != 1 {
		t.Fatalf("listed: %+v", listed)
	}

	if _, out, _ := run(t, "list", "--category", "nope"); !strings.Contains(out, "no tools") {
		t.Fatalf("category filter: %s", out)
	}

	code, out, _ = run(t, "info", "hello", "--json")
	var info struct {
		Ref           string
		DirectCommand []string `json:"direct_command"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &info) != nil || info.Ref != "personal/hello" {
		t.Fatalf("info: code=%d out=%s", code, out)
	}
	if len(info.DirectCommand) != 1 || !strings.HasSuffix(info.DirectCommand[0], filepath.Join("hello", "run")) {
		t.Fatalf("direct command: %v", info.DirectCommand)
	}

	code, out, _ = run(t, "info", "hello")
	if code != 0 || !strings.Contains(out, "maggot run personal/hello") || !strings.Contains(out, "<positional>") {
		t.Fatalf("info text: %s", out)
	}

	if code, _, e := run(t, "info", "nope"); code == 0 || !strings.Contains(e, "no tool") {
		t.Fatalf("missing tool: %s", e)
	}
}

func TestToolboxFlagAndCwdDetection(t *testing.T) {
	isolate(t)
	unregistered := filepath.Join(t.TempDir(), "repo")
	if code, _, e := run(t, "toolbox", "init", unregistered, "--name", "repo", "--no-register"); code != 0 {
		t.Fatal(e)
	}
	addTool(t, unregistered, "hello", helloManifest, helloScript)

	// --toolbox with a path
	if code, out, _ := run(t, "--toolbox", unregistered, "list"); code != 0 || !strings.Contains(out, "repo/hello") {
		t.Fatalf("--toolbox path: %s", out)
	}
	// detected from cwd
	t.Chdir(filepath.Join(unregistered, "tools"))
	if code, out, _ := run(t, "list"); code != 0 || !strings.Contains(out, "repo/hello") {
		t.Fatalf("cwd detection: %s", out)
	}
}
