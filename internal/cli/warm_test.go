package cli

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestWarmSurvivesBadClients is the regression test for a warm process that
// crashed when a status check hung up before reading the answer.
func TestWarmSurvivesBadClients(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	helper, err := os.ReadFile("../scaffold/templates/python-warm/maggot_warm.py")
	if err != nil {
		t.Fatal(err)
	}
	isolate(t)
	tb := newToolbox(t, "main")
	dir := filepath.Join(tb, "tools", "sleepy")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "maggot_warm.py"), helper, 0o644)
	os.WriteFile(filepath.Join(dir, "run.py"), []byte(`import sys, time, maggot_warm
def load(): return {}
def handle(argv, state):
    if argv[:1] == ["sleep"]:
        time.sleep(float(argv[1]))
    print("done")
    return 0
sys.exit(maggot_warm.main(load, handle))
`), 0o755)
	os.WriteFile(filepath.Join(dir, "tool.json"), []byte(`{"schema":1,"id":"sleepy","version":"1.0.0","name":"S","description":"d",
	  "entry":{"path":"run.py","interpreter":["python3"]},"warm":{"args":["--serve"]}}`), 0o644)
	t.Cleanup(func() { warm.Stop("sleepy") })

	if code, _, e := run(t, "warm", "sleepy", "--for", "2m"); code != 0 {
		t.Fatal(e)
	}
	sock, _ := warm.SocketPath("sleepy")
	// Clients that hang up without reading the answer.
	for i := 0; i < 20; i++ {
		c, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		c.Write([]byte(`{"type":"status"}` + "\n"))
		c.Close()
	}
	// Junk does not hurt either.
	if c, err := net.Dial("unix", sock); err == nil {
		c.Write([]byte("not json\n"))
		c.Close()
	}
	// A long job; status during it must answer at once and say busy.
	done := make(chan string)
	go func() {
		_, out, _ := run(t, "run", "sleepy", "--", "sleep", "2")
		done <- out
	}()
	time.Sleep(700 * time.Millisecond)
	start := time.Now()
	st, _ := warm.Get("sleepy")
	if st == nil || !st.Running || !st.Busy || time.Since(start) > time.Second {
		t.Fatalf("status during job: %+v after %v", st, time.Since(start))
	}
	if out := <-done; out != "done\n" {
		t.Fatalf("job output %q", out)
	}
	st, _ = warm.Get("sleepy")
	if st == nil || !st.Running || st.Busy || st.Jobs != 1 {
		t.Fatalf("after job: %+v", st)
	}
}
