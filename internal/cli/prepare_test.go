package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepare(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "main")
	addTool(t, tb, "maker", makerManifest, makerScript)
	addTool(t, tb, "needs", needsManifest, "#!/bin/sh\n")

	decode := func(out string) prepareResult {
		var r prepareResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return r
	}

	// Ready: inputs + an output folder → exact command and next step.
	out := filepath.Join(t.TempDir(), "assets")
	code, stdout, _ := run(t, "prepare", "maker", "--inputs", `{"name":"logo"}`, "--out", out, "--json")
	r := decode(stdout)
	if code != 0 || !r.Ready || r.Output["dir"] != out || !strings.HasSuffix(r.Command, "--out "+out+" logo") ||
		!strings.Contains(r.Next, "maggot run main/maker --json --inputs") || r.Inputs["name"] != "logo" {
		t.Fatalf("ready: %d %+v", code, r)
	}

	// Missing input → invalid_input, exit 1, no next step.
	code, stdout, _ = run(t, "prepare", "maker", "--inputs", `{}`, "--json")
	r = decode(stdout)
	if code != 1 || r.Ready || r.Problems[0].Kind != "invalid_input" || !strings.Contains(r.Problems[0].Message, "name: is required") || r.Next != "" {
		t.Fatalf("invalid: %d %+v", code, r)
	}

	// No output folder chosen → not ready: a decision, and the next command
	// has a placeholder so it cannot be run as is.
	code, stdout, _ = run(t, "prepare", "maker", "--json", "--", "logo")
	r = decode(stdout)
	if code != 1 || r.Ready || len(r.Decisions) != 1 || r.Decisions[0].Kind != "output_folder" ||
		!strings.HasSuffix(r.Next, "-- --out '<choose a folder>' logo") {
		t.Fatalf("raw: %d %+v", code, r)
	}
	_, stdout, _ = run(t, "prepare", "maker", "--json", "--inputs", `{"name":"x"}`)
	if r = decode(stdout); r.Ready || !strings.HasSuffix(r.Next, "--out '<choose a folder>'") {
		t.Fatalf("inputs without out: %+v", r)
	}
	// Raw args with the tool's own output flag: decided, ready.
	_, stdout, _ = run(t, "prepare", "maker", "--json", "--", "--out", out, "logo")
	if r = decode(stdout); !r.Ready || len(r.Decisions) != 0 || r.Output["dir"] != out {
		t.Fatalf("raw with out: %+v", r)
	}

	// Missing binary and secret → problems with fixes.
	code, stdout, _ = runApp(t, "", func(a *app) { a.doctor = fakeChecker(map[string]string{"brew": ""}) }, "prepare", "needs", "--json")
	r = decode(stdout)
	kinds := map[string]Problem{}
	for _, p := range r.Problems {
		kinds[p.Kind] = p
	}
	if code != 1 || !strings.Contains(kinds["missing_requirement"].Fix, "brew install fakebin") ||
		!strings.Contains(kinds["missing_env"].Fix, "maggot secret set main/needs NEEDS_TOKEN") {
		t.Fatalf("missing: %+v", r)
	}

	// --review needs an output folder flag; text output works.
	if code, stdout, _ := run(t, "prepare", "needs", "--review"); code != 1 || !strings.Contains(stdout, "cannot be held for review") {
		t.Fatalf("review: %s", stdout)
	}
	if code, stdout, _ := run(t, "prepare", "maker", "--review", "--inputs", `{"name":"x"}`); code != 0 || !strings.Contains(stdout, "next:    maggot run main/maker --json --review") {
		t.Fatalf("review ok: %s", stdout)
	}
}
