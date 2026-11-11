package cli

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/secrets"
)

const needsManifest = `{
  "schema": 1, "id": "needs", "version": "1.0.0", "name": "Needs", "description": "d",
  "entry": {"path": "run"},
  "requires": [
    {"bin": "fakebin", "version": ">=2", "install": {"brew": "fakebin", "apt": "fakebin"}}
  ],
  "env": [
    {"name": "NEEDS_TOKEN", "secret": true, "required": true},
    {"name": "NEEDS_REGION"}
  ]
}`

// fakeChecker pretends the given binaries exist with the given version output.
func fakeChecker(bins map[string]string) *doctor.Checker {
	return &doctor.Checker{
		GOOS: "darwin",
		LookPath: func(name string) (string, error) {
			if _, ok := bins[name]; ok {
				return "/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		Output: func(_ context.Context, name string, _ ...string) (string, error) {
			return bins[filepath.Base(name)], nil
		},
	}
}

func TestDoctorReportsAndInstalls(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "p")
	addTool(t, tb, "needs", needsManifest, "#!/bin/sh\n")
	addTool(t, tb, "hello", helloManifest, helloScript)
	store := &secrets.FileStore{Path: filepath.Join(t.TempDir(), "s.json")}
	bins := map[string]string{"brew": ""}
	var installed [][]string
	no := false
	setup := func(a *app) {
		a.doctor = fakeChecker(bins)
		a.secrets = store
		a.forceInteractive = &no
		a.installer = func(argv []string) error {
			installed = append(installed, argv)
			bins["fakebin"] = "fakebin 2.1.0"
			return nil
		}
	}

	// All tools: hello is fine, needs is not → exit 1.
	code, out, _ := runApp(t, "", setup, "doctor")
	if code != 1 || !strings.Contains(out, "p/hello: ok") || !strings.Contains(out, "install: brew install fakebin") ||
		!strings.Contains(out, "maggot secret set p/needs NEEDS_TOKEN") {
		t.Fatalf("code=%d out=%s", code, out)
	}

	// --install without a terminal and without --yes must refuse.
	code, _, errOut := runApp(t, "", setup, "doctor", "needs", "--install")
	if code != 1 || !strings.Contains(errOut, "--yes") || len(installed) != 0 {
		t.Fatalf("should refuse: code=%d err=%s installed=%v", code, errOut, installed)
	}

	// With --yes it installs, then re-checks. The secret is still missing.
	code, out, _ = runApp(t, "", setup, "doctor", "needs", "--install", "--yes", "--json")
	if len(installed) != 1 || strings.Join(installed[0], " ") != "brew install fakebin" {
		t.Fatalf("installed: %v", installed)
	}
	var res struct {
		OK    bool
		Tools []toolHealth
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err, out)
	}
	if code != 1 || res.OK || !res.Tools[0].Requirements[0].OK() {
		t.Fatalf("after install: code=%d %+v", code, res)
	}

	// Save the secret through the CLI, then everything is ok.
	if code, _, e := runApp(t, "tok123\n", setup, "secret", "set", "needs", "NEEDS_TOKEN"); code != 0 {
		t.Fatal(e)
	}
	if v, _, _ := store.Get("needs", "NEEDS_TOKEN"); v != "tok123" {
		t.Fatalf("stored %q", v)
	}
	if code, out, _ := runApp(t, "", setup, "doctor", "needs"); code != 0 {
		t.Fatalf("should be ok now: %s", out)
	}
}

func TestSecretCommandRules(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "p")
	addTool(t, tb, "needs", needsManifest, "#!/bin/sh\n")
	store := &secrets.FileStore{Path: filepath.Join(t.TempDir(), "s.json")}
	setup := func(a *app) { a.secrets = store }

	if code, _, e := runApp(t, "x", setup, "secret", "set", "needs", "NEEDS_REGION"); code == 0 || !strings.Contains(e, "not marked secret") {
		t.Fatalf("non-secret: %s", e)
	}
	if code, _, e := runApp(t, "x", setup, "secret", "set", "needs", "OTHER"); code == 0 || !strings.Contains(e, "does not declare") {
		t.Fatalf("undeclared: %s", e)
	}
	runApp(t, "abc", setup, "secret", "set", "needs", "NEEDS_TOKEN")
	code, out, _ := runApp(t, "", setup, "secret", "list", "needs")
	if code != 0 || !strings.Contains(out, "NEEDS_TOKEN\tset (store)") || strings.Contains(out, "abc") {
		t.Fatalf("list: %s", out)
	}
	t.Setenv("NEEDS_REGION", "eu")
	_, out, _ = runApp(t, "", setup, "secret", "list", "needs")
	if !strings.Contains(out, "NEEDS_REGION\tset (env)") {
		t.Fatalf("env source: %s", out)
	}
	if code, _, _ := runApp(t, "", setup, "secret", "rm", "needs", "NEEDS_TOKEN"); code != 0 {
		t.Fatal("rm failed")
	}
}
