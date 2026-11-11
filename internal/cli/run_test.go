package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/secrets"
)

func TestRunPassesArgsAndExitCode(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "p")
	addTool(t, tb, "hello", helloManifest, helloScript)
	addTool(t, tb, "fail", `{"schema":1,"id":"fail","version":"1.0.0","name":"F","description":"d","entry":{"path":"run"}}`,
		"#!/bin/sh\necho oops >&2\nexit 3\n")

	if code, out, _ := run(t, "run", "hello", "world"); code != 0 || out != "hello world\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	// "--" separator and tool flags that look like maggot flags
	if code, out, _ := run(t, "run", "hello", "--", "--shout", "world"); code != 0 || out != "HELLO world\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if code, out, _ := run(t, "run", "hello", "--shout", "x"); code != 0 || out != "HELLO x\n" {
		t.Fatalf("interspersed: code=%d out=%q", code, out)
	}
	code, _, errOut := run(t, "run", "fail")
	if code != 3 || !strings.Contains(errOut, "oops") || strings.Contains(errOut, "error:") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestRunWithInputs(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "p")
	addTool(t, tb, "hello", helloManifest, helloScript)

	if code, out, e := run(t, "run", "--inputs", `{"name":"x","shout":true}`, "hello"); code != 0 || out != "HELLO x\n" {
		t.Fatalf("code=%d out=%q err=%s", code, out, e)
	}
	file := filepath.Join(t.TempDir(), "in.json")
	os.WriteFile(file, []byte(`{"name":"file"}`), 0o644)
	if code, out, _ := run(t, "run", "--inputs", "@"+file, "hello"); code != 0 || out != "hello file\n" {
		t.Fatalf("@file: code=%d out=%q", code, out)
	}
	if code, _, e := run(t, "run", "--inputs", `{}`, "hello"); code == 0 || !strings.Contains(e, "name: is required") {
		t.Fatalf("validation: %s", e)
	}
	if code, _, e := run(t, "run", "--inputs", `{"name":"x"}`, "hello", "extra"); code == 0 || !strings.Contains(e, "not both") {
		t.Fatalf("both: %s", e)
	}
	code, out, _ := run(t, "run", "--dry-run", "--inputs", `{"name":"a b"}`, "hello")
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "run 'a b'") {
		t.Fatalf("dry run: %q", out)
	}
}

func TestRunPreflightAndSecrets(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "p")
	m := `{"schema":1,"id":"sec","version":"1.0.0","name":"S","description":"d","entry":{"path":"run"},
	  "requires":[{"bin":"sh"}],
	  "env":[{"name":"SEC_TOKEN","secret":true,"required":true}]}`
	addTool(t, tb, "sec", m, "#!/bin/sh\necho \"token=$SEC_TOKEN tool=$MAGGOT_TOOL_ID\"\n")
	store := &secrets.FileStore{Path: filepath.Join(t.TempDir(), "s.json")}
	setup := func(a *app) { a.secrets = store }

	code, _, e := runApp(t, "", setup, "run", "sec")
	if code != 1 || !strings.Contains(e, "missing: env SEC_TOKEN") || !strings.Contains(e, "maggot doctor p/sec") {
		t.Fatalf("preflight: code=%d err=%s", code, e)
	}
	store.Set("sec", "SEC_TOKEN", "abc")
	if code, out, _ := runApp(t, "", setup, "run", "sec"); code != 0 || out != "token=abc tool=sec\n" {
		t.Fatalf("secret: code=%d out=%q", code, out)
	}
	// The environment wins over the stored secret.
	t.Setenv("SEC_TOKEN", "from-env")
	if _, out, _ := runApp(t, "", setup, "run", "sec"); out != "token=from-env tool=sec\n" {
		t.Fatalf("env wins: %q", out)
	}

	addTool(t, tb, "nobin", `{"schema":1,"id":"nobin","version":"1.0.0","name":"N","description":"d","entry":{"path":"run"},"requires":[{"bin":"definitely-not-installed-xyz"}]}`, "#!/bin/sh\necho ran\n")
	if code, _, e := run(t, "run", "nobin"); code != 1 || !strings.Contains(e, "binary definitely-not-installed-xyz") {
		t.Fatalf("missing bin: %s", e)
	}
	if code, out, _ := run(t, "run", "--skip-checks", "nobin"); code != 0 || out != "ran\n" {
		t.Fatalf("skip checks: %q", out)
	}
}
