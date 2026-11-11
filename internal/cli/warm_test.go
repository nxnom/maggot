package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/warm"
)

func TestWarmEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not installed")
	}
	isolate(t)
	newToolbox(t, "main")
	drafts := filepath.Join(os.Getenv("MAGGOT_DATA_DIR"), "drafts")
	if code, _, e := run(t, "new", "warmy", "--lang", "python", "--warm"); code != 0 {
		t.Fatal(e)
	}
	if code, _, e := run(t, "import", "--yes", filepath.Join(drafts, "warmy")); code != 0 {
		t.Fatal(e)
	}
	t.Cleanup(func() { warm.Stop("warmy") })

	// Cold run works before warming.
	work := t.TempDir()
	t.Chdir(work)
	os.WriteFile("a.txt", []byte("hi\n"), 0o644)
	if code, out, e := run(t, "run", "warmy", "--", "--upper", "--out", "cold", "a.txt"); code != 0 || !strings.Contains(out, "a.out.txt") {
		t.Fatalf("cold: %d %s %s", code, out, e)
	}

	if code, out, e := run(t, "warm", "warmy", "--for", "2m", "--wait", "2m"); code != 0 || !strings.Contains(out, "is warm") {
		t.Fatalf("warm: %d %s %s", code, out, e)
	}
	if code, _, e := run(t, "warm", "warmy"); code == 0 || !strings.Contains(e, "already warm") {
		t.Fatalf("second warm should refuse: %s", e)
	}

	// Two runs go to the warm process: relative paths resolve from here.
	for i := 0; i < 2; i++ {
		code, out, e := run(t, "run", "warmy", "--", "--upper", "--out", "hot", "a.txt")
		if code != 0 || !strings.Contains(out, "a.out.txt") {
			t.Fatalf("warm run: %d %q %s", code, out, e)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(work, "hot", "a.out.txt")); string(b) != "HI\n" {
		t.Fatalf("warm output: %q", b)
	}
	// Errors and exit codes come back through the socket.
	code, _, e := run(t, "run", "warmy", "--", "missing.txt")
	if code != 2 || !strings.Contains(e, "is not a file") {
		t.Fatalf("warm error: %d %s", code, e)
	}

	_, out, _ := run(t, "warm", "list", "--json")
	var listed struct {
		Warm []warm.Status
	}
	json.Unmarshal([]byte(out), &listed)
	if len(listed.Warm) != 1 || !listed.Warm[0].Running || listed.Warm[0].Jobs != 3 || listed.Warm[0].IdleLeft <= 0 {
		t.Fatalf("list: %s", out)
	}
	if code, _, e := run(t, "warm", "stop", "warmy"); code != 0 {
		t.Fatal(e)
	}
	if _, out, _ := run(t, "warm", "list"); !strings.Contains(out, "no warm tools") {
		t.Fatalf("after stop: %s", out)
	}
	// After stopping, runs are cold again and still work.
	if code, _, e := run(t, "run", "warmy", "--", "--out", "again", "a.txt"); code != 0 {
		t.Fatalf("cold after stop: %s", e)
	}
	if code, _, e := run(t, "warm", "hello-nope"); code == 0 {
		t.Fatalf("unknown tool: %s", e)
	}
}

func TestWarmNeedsSupport(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "main")
	addTool(t, tb, "hello", helloManifest, helloScript)
	if code, _, e := run(t, "warm", "hello"); code == 0 || !strings.Contains(e, "cannot stay warm") {
		t.Fatalf("hello: %s", e)
	}
}
