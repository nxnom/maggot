package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPresetsCLI(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "main")
	toolDir := addTool(t, tb, "hello", helloManifest, helloScript)

	if code, _, e := run(t, "presets", "save", "hello", "loud", "--inputs", `{"shout":true}`, "--toolbox"); code != 0 {
		t.Fatal(e)
	}
	if _, err := os.Stat(filepath.Join(toolDir, "presets", "loud.json")); err != nil {
		t.Fatal("toolbox preset should be in the tool folder")
	}
	code, out, _ := run(t, "presets", "save", "hello", "World Greeting", "--inputs", `{"name":"world"}`)
	if code != 0 || !strings.Contains(out, "personal/world-greeting") {
		t.Fatalf("personal save: %s", out)
	}
	if code, _, e := run(t, "presets", "save", "hello", "x", "--inputs", `{"colour":1}`); code == 0 || !strings.Contains(e, `no input "colour"`) {
		t.Fatalf("unknown input: %s", e)
	}

	// Run with a preset; --inputs override and add values.
	if code, out, _ := run(t, "run", "hello", "--preset", "world-greeting"); code != 0 || out != "hello world\n" {
		t.Fatalf("preset run: %q", out)
	}
	if code, out, _ := run(t, "run", "hello", "--preset", "loud", "--inputs", `{"name":"ann"}`); code != 0 || out != "HELLO ann\n" {
		t.Fatalf("preset + override: %q", out)
	}
	if code, _, e := run(t, "run", "hello", "--preset", "nope"); code == 0 || !strings.Contains(e, "available: personal/world-greeting, toolbox/loud") {
		t.Fatalf("unknown preset: %s", e)
	}

	// Personal wins over toolbox on the same name; scope prefixes pick one.
	run(t, "presets", "save", "hello", "loud", "--inputs", `{"name":"mine"}`)
	if _, out, _ := run(t, "run", "hello", "--preset", "loud"); out != "hello mine\n" {
		t.Fatalf("personal first: %q", out)
	}
	if _, out, _ := run(t, "run", "hello", "--preset", "toolbox/loud", "--inputs", `{"name":"x"}`); out != "HELLO x\n" {
		t.Fatalf("toolbox prefix: %q", out)
	}

	// prepare keeps --preset in the next command; info lists presets.
	_, out, _ = run(t, "prepare", "hello", "--preset", "loud", "--inputs", `{"name":"z"}`, "--json")
	var r prepareResult
	json.Unmarshal([]byte(out), &r)
	if !r.Ready || r.Inputs["name"] != "z" || !strings.Contains(r.Next, "--preset loud --inputs") || strings.Contains(r.Next, "mine") {
		t.Fatalf("prepare: %+v", r)
	}
	_, out, _ = run(t, "info", "hello", "--json")
	if !strings.Contains(out, `"use": "personal/loud"`) || !strings.Contains(out, `"use": "toolbox/loud"`) {
		t.Fatalf("info presets: %s", out)
	}
	if _, out, _ := run(t, "presets", "hello"); !strings.Contains(out, "personal/world-greeting") || !strings.Contains(out, "toolbox/loud") {
		t.Fatalf("list: %s", out)
	}
	if code, _, _ := run(t, "presets", "rm", "hello", "personal/loud"); code != 0 {
		t.Fatal("rm")
	}
	if _, out, _ := run(t, "run", "hello", "--preset", "loud", "--inputs", `{"name":"y"}`); out != "HELLO y\n" {
		t.Fatalf("after rm the toolbox preset is used: %q", out)
	}
}
