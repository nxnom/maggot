package ui

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the `maggot ui-run` worker.
const workerEnv = "MAGGOT_TEST_UI_WORKER"

func TestMain(m *testing.M) {
	if os.Getenv(workerEnv) == "1" {
		if err := RunWorker(os.Args[len(os.Args)-1]); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const slowManifest = `{"schema":1,"id":"slow","version":"1.0.0","name":"Slow","description":"d",
  "entry":{"path":"run"},
  "inputs":[{"name":"secs","type":"string","required":true,"arg":{"positional":true}}],
  "outputs":{"kind":"files","dir_flag":"--out"}}`

const slowScript = `#!/bin/sh
[ "$1" = "--out" ] && { out="$2"; shift 2; }
echo "start"
sleep "$1"
echo "secret=$SLOW_SECRET"
echo done > "$out/done.txt"
`

func detachedServer(t *testing.T) *testServer {
	t.Helper()
	ts := newTestServer(t)
	tool := makeTool(t, slowManifest, slowScript)
	dst := filepath.Join(ts.tb, "tools", "slow")
	os.MkdirAll(dst, 0o755)
	os.WriteFile(filepath.Join(dst, "tool.json"), []byte(slowManifest), 0o644)
	data, _ := os.ReadFile(filepath.Join(tool.Dir, "run"))
	os.WriteFile(filepath.Join(dst, "run"), data, 0o755)
	t.Setenv(workerEnv, "1")
	ts.s.Worker = []string{os.Args[0]}
	return ts
}

func waitStatus(t *testing.T, m *Manager, id string, d time.Duration) RunMeta {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		meta, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Status != StatusRunning || time.Now().After(deadline) {
			return meta
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDetachedRunOutlivesUI(t *testing.T) {
	ts := detachedServer(t)
	var meta RunMeta
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "slow", "values": map[string]any{"secs": "1"}}, 200, &meta)
	if !meta.Detached || meta.Status != StatusRunning || meta.PID == os.Getpid() || meta.PID == 0 {
		t.Fatalf("meta: %+v", meta)
	}

	// The stream follows the run folder the worker writes.
	resp, err := ts.client.Get(ts.URL + "/api/runs/" + meta.ID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	sawStart := false
	for sc.Scan() {
		if line, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var m Message
			json.Unmarshal([]byte(line), &m)
			if m.Text == "start" {
				sawStart = true
				break
			}
		}
	}
	resp.Body.Close()
	if !sawStart {
		t.Fatal("stream did not show the first line")
	}

	// Stopping the UI must not stop the run.
	ts.s.Close()
	ts.Close()
	final := waitStatus(t, ts.s.runs, meta.ID, 10*time.Second)
	if final.Status != StatusSucceeded {
		t.Fatalf("run after UI stop: %+v", final)
	}
	if _, err := os.Stat(filepath.Join(final.OutDir, "done.txt")); err != nil {
		t.Fatal("tool did not finish:", err)
	}
	if _, err := os.Stat(filepath.Join(ts.s.runs.Root, ".spec-"+meta.ID+".json")); err == nil {
		t.Fatal("spec file left behind")
	}
}

func TestDetachedRunCancelAndEnv(t *testing.T) {
	ts := detachedServer(t)
	var meta RunMeta
	ts.do(t, "POST", "/api/runs", map[string]any{"ref": "slow", "values": map[string]any{"secs": "30"}}, 200, &meta)
	ts.do(t, "DELETE", "/api/runs/"+meta.ID, nil, 400, nil) // still running
	ts.do(t, "POST", "/api/runs/"+meta.ID+"/cancel", nil, 200, nil)
	final := waitStatus(t, ts.s.runs, meta.ID, 10*time.Second)
	if final.Status != StatusCancelled {
		t.Fatalf("after cancel: %+v", final)
	}
	if _, err := os.Stat(filepath.Join(final.Dir, cancelFile)); err == nil {
		t.Fatal("cancel file left behind")
	}
	ts.do(t, "POST", "/api/runs/"+meta.ID+"/cancel", nil, 400, nil)

	// Secrets reach the tool through the worker's environment.
	ws, tl, err := ts.s.resolve(nil, "slow")
	if err != nil || ws == nil {
		t.Fatal(err)
	}
	m2, err := ts.s.startDetached(tl, StartOptions{Values: map[string]any{"secs": "0"}, ExtraEnv: []string{"SLOW_SECRET=s3"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitStatus(t, ts.s.runs, m2.ID, 10*time.Second); got.Status != StatusSucceeded {
		t.Fatalf("env run: %+v", got)
	}
	log, _ := ts.s.runs.Log(m2.ID)
	found := false
	for _, l := range log {
		found = found || l.Text == "secret=s3"
	}
	if !found {
		t.Fatalf("secret not passed: %+v", log)
	}
}
