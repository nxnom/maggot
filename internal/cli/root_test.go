package cli

import (
	"bytes"
	"strings"
	"testing"
)

// run executes the CLI in-process and returns exit code, stdout and stderr.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// runApp is like run but lets the test set hooks on the app first.
func runApp(t *testing.T, stdin string, setup func(a *app), args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &app{stdin: strings.NewReader(stdin), stdout: &out, stderr: &errOut}
	if setup != nil {
		setup(a)
	}
	code := a.execute(args)
	return code, out.String(), errOut.String()
}

func TestVersionFlag(t *testing.T) {
	code, out, _ := run(t, "--version")
	if code != 0 || !strings.Contains(out, "maggot version") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errOut := run(t, "nope")
	if code != 1 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}
