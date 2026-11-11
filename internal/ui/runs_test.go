package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/toolbox"
)

// makeTool writes a shell tool and returns it loaded.
func makeTool(t *testing.T, manifestJSON, script string) *toolbox.Tool {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell tools")
	}
	tb := &toolbox.Toolbox{Handle: "t", Path: t.TempDir()}
	m, err := manifest.Parse([]byte(manifestJSON))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tb.Path, "tools", m.ID)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755)
	return &toolbox.Tool{Toolbox: tb, Dir: dir, Manifest: m}
}

const workerManifest = `{"schema":1,"id":"worker","version":"1.0.0","name":"Worker","description":"d",
  "entry":{"path":"run"},
  "inputs":[{"name":"name","type":"string","required":true,"arg":{"positional":true}}],
  "outputs":{"kind":"files","dir_flag":"--out","progress":{"regex":"([0-9]+)% done"}}}`

const workerScript = `#!/bin/sh
[ "$1" = "--out" ] && { out="$2"; shift 2; }
printf '10%% done\r50%% done\r'
echo
echo "warn: $1" >&2
echo '{"type":"progress","value":0.75}' >> "$MAGGOT_EVENTS"
echo '{"type":"output","path":"x"}' >> "$MAGGOT_EVENTS"
echo "hello $1" > "$out/$1.txt"
mkdir -p "$out/sub" && echo nested > "$out/sub/n.txt"
echo "cwd=$(pwd)"
[ "$1" = "fail" ] && exit 4
exit 0
`

func waitRun(t *testing.T, r *Run) RunMeta {
	t.Helper()
	select {
	case <-r.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("run did not finish")
	}
	return r.Meta()
}

func TestRunLifecycle(t *testing.T) {
	tool := makeTool(t, workerManifest, workerScript)
	m := NewManager(t.TempDir(), 10)
	r, err := m.Start(tool, map[string]any{"name": "ann"}, []string{"EXTRA=1"})
	if err != nil {
		t.Fatal(err)
	}
	hist, ch, stop := r.Subscribe()
	defer stop()
	meta := waitRun(t, r)
	if meta.Status != StatusSucceeded || *meta.ExitCode != 0 || meta.Progress != 1 {
		t.Fatalf("meta: %+v", meta)
	}

	// Collect everything the subscriber saw.
	msgs := hist
	for len(ch) > 0 {
		msgs = append(msgs, <-ch)
	}
	var progress []float64
	var out, errs []string
	events := 0
	for _, msg := range msgs {
		switch msg.Type {
		case "progress":
			progress = append(progress, msg.Value)
		case "out":
			out = append(out, msg.Text)
		case "err":
			errs = append(errs, msg.Text)
		case "event":
			events++
		}
	}
	if len(progress) < 3 || progress[0] != 0.1 || progress[1] != 0.5 {
		t.Errorf("progress: %v", progress)
	}
	if events != 2 || len(errs) != 1 || errs[0] != "warn: ann" {
		t.Errorf("events=%d errs=%v", events, errs)
	}
	realOut, _ := filepath.EvalSymlinks(meta.OutDir)
	if !strings.Contains(strings.Join(out, "\n"), "cwd="+realOut) && !strings.Contains(strings.Join(out, "\n"), "cwd="+meta.OutDir) {
		t.Errorf("tool should run inside the out dir: %v", out)
	}

	files := m.Outputs(meta)
	if len(files) != 2 || files[0].Mime != "text/plain" {
		t.Fatalf("outputs: %+v", files)
	}

	// Finished runs load from disk, with their log.
	got, err := m.Get(meta.ID)
	if err != nil || got.Status != StatusSucceeded || got.ToolID != "worker" {
		t.Fatalf("get: %+v %v", got, err)
	}
	log, err := m.Log(meta.ID)
	if err != nil || len(log) < 3 {
		t.Fatalf("log: %v %v", log, err)
	}
	if !strings.Contains(m.stdoutText(got), "50% done") {
		t.Fatal("stdout.txt")
	}
	if list := m.List("worker", 0); len(list) != 1 || list[0].ID != meta.ID {
		t.Fatalf("list: %+v", list)
	}
}

func TestRunFailureValidationAndPrune(t *testing.T) {
	tool := makeTool(t, workerManifest, workerScript)
	m := NewManager(t.TempDir(), 2)
	if _, err := m.Start(tool, map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "name: is required") {
		t.Fatalf("validation: %v", err)
	}
	r, _ := m.Start(tool, map[string]any{"name": "fail"}, nil)
	if meta := waitRun(t, r); meta.Status != StatusFailed || *meta.ExitCode != 4 {
		t.Fatalf("fail: %+v", meta)
	}
	for i := 0; i < 3; i++ {
		r, _ := m.Start(tool, map[string]any{"name": "ok"}, nil)
		waitRun(t, r)
		time.Sleep(1100 * time.Millisecond) // run ids sort by second
	}
	if got := m.List("worker", 0); len(got) != 2 {
		t.Fatalf("prune should keep 2, got %d", len(got))
	}
}

func TestRunCancel(t *testing.T) {
	tool := makeTool(t, `{"schema":1,"id":"slow","version":"1.0.0","name":"S","description":"d","entry":{"path":"run"}}`,
		"#!/bin/sh\necho started\nsleep 30 &\nwait\n")
	m := NewManager(t.TempDir(), 5)
	r, err := m.Start(tool, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := m.Cancel(r.Meta().ID); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	meta := waitRun(t, r)
	if meta.Status != StatusCancelled || time.Since(start) > 5*time.Second {
		t.Fatalf("cancel: %+v after %v", meta, time.Since(start))
	}
	if err := m.Cancel(meta.ID); err == nil {
		t.Fatal("cancel of finished run should fail")
	}
	if err := m.Delete(meta.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(meta.ID); err == nil {
		t.Fatal("deleted run should be gone")
	}
	if _, err := m.Get("../../etc"); err == nil {
		t.Fatal("bad id must fail")
	}
}

func TestSplitLines(t *testing.T) {
	data := []byte("a\rb\r\nc\nd")
	var got []string
	for len(data) > 0 {
		adv, tok, _ := splitLines(data, true)
		got = append(got, string(tok))
		data = data[adv:]
	}
	if strings.Join(got, ",") != "a,b,c,d" {
		t.Fatalf("got %q", got)
	}
}
