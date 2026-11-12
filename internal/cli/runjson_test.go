package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func decodeResult(t *testing.T, out string) runResult {
	t.Helper()
	var r runResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("stdout must be exactly one JSON object: %v\n%s", err, out)
	}
	return r
}

func TestRunJSON(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "main")
	addTool(t, tb, "maker", makerManifest, makerScript)
	addTool(t, tb, "hello", helloManifest, helloScript)
	addTool(t, tb, "fail", `{"schema":1,"id":"fail","version":"1.0.0","name":"F","description":"d","entry":{"path":"run"}}`,
		"#!/bin/sh\necho 'working...'\necho 'error: the input is broken' >&2\nexit 3\n")
	addTool(t, tb, "needs", needsManifest, "#!/bin/sh\n")

	// Success: files found in the output folder; tool output went to stderr.
	out := filepath.Join(t.TempDir(), "assets")
	code, stdout, stderr := run(t, "run", "maker", "--json", "--inputs", `{"name":"logo"}`, "--out", out)
	r := decodeResult(t, stdout)
	if code != 0 || !r.OK || r.Category != "ok" || r.OutputDir != out || len(r.Files) != 1 ||
		r.Files[0].Path != filepath.Join(out, "logo.txt") || !strings.Contains(stderr, "making logo") {
		t.Fatalf("ok: %d %+v stderr=%q", code, r, stderr)
	}

	// A tool whose result is text: stdout is captured in the result.
	_, stdout, _ = run(t, "run", "hello", "--json", "--", "world")
	if r = decodeResult(t, stdout); !r.OK || r.Stdout != "hello world\n" {
		t.Fatalf("stdout: %+v", r)
	}

	// Tool failure: exit code, error lines and next steps.
	code, stdout, _ = run(t, "run", "fail", "--json")
	r = decodeResult(t, stdout)
	if code != 3 || r.OK || r.Category != "tool_failed" || r.ExitCode != 3 ||
		len(r.StderrTail) != 1 || r.StderrTail[0] != "error: the input is broken" || len(r.Next) == 0 {
		t.Fatalf("fail: %d %+v", code, r)
	}

	// Problems found before running.
	code, stdout, _ = run(t, "run", "maker", "--json", "--inputs", `{}`)
	if r = decodeResult(t, stdout); code != 1 || r.Category != "invalid_input" || len(r.Problems) != 1 {
		t.Fatalf("invalid: %d %+v", code, r)
	}
	code, stdout, _ = runApp(t, "", func(a *app) { a.doctor = fakeChecker(map[string]string{"brew": ""}) }, "run", "needs", "--json")
	if r = decodeResult(t, stdout); code != 1 || r.Category != "missing_requirement" || !strings.Contains(strings.Join(r.Next, " "), "brew install fakebin") {
		t.Fatalf("missing: %d %+v", code, r)
	}

	// Review run: run id, files in the held folder and the take/rm steps.
	code, stdout, stderr = run(t, "run", "maker", "--json", "--review", "--", "draft")
	r = decodeResult(t, stdout)
	if code != 0 || !r.OK || !r.Review || r.RunID == "" || len(r.Files) != 1 ||
		!strings.Contains(strings.Join(r.Next, "\n"), "maggot take "+r.RunID) || !strings.Contains(stderr, "making draft") {
		t.Fatalf("review: %d %+v", code, r)
	}
}

func TestTailBuffer(t *testing.T) {
	tb := &tailBuffer{n: 2}
	tb.Write([]byte("a\nb\r c\nd"))
	tb.Write([]byte("e\n"))
	if got := strings.Join(tb.result(), "|"); got != " c|de" {
		t.Fatalf("got %q", got)
	}
}
