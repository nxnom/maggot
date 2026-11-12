package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchCommand(t *testing.T) {
	isolate(t)
	tb := newToolbox(t, "p")
	addTool(t, tb, "hello", helloManifest, helloScript)
	addTool(t, tb, "bg-remover", `{"schema":1,"id":"bg-remover","version":"1.0.0","name":"Background Remover",
	  "description":"Remove the background from photos.","tags":["transparent","cutout"],"entry":{"path":"run"}}`, "#!/bin/sh\n")

	code, out, _ := run(t, "search", "remove", "image", "background")
	if code != 0 || !strings.Contains(out, "p/bg-remover") || strings.Contains(out, "p/hello") {
		t.Fatalf("search: %s", out)
	}
	code, out, _ = run(t, "search", "greeting", "--json")
	var res struct {
		Results []struct {
			Ref     string
			Matched []string
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || len(res.Results) != 1 || res.Results[0].Ref != "p/hello" {
		t.Fatalf("json: %s", out)
	}
	if _, out, _ := run(t, "search", "spreadsheet"); !strings.Contains(out, "no matching tools") {
		t.Fatalf("no match: %s", out)
	}
	if _, out, _ := run(t, "search", "backgrnd removr"); !strings.Contains(out, "closest (not a direct match):") || !strings.Contains(out, "p/bg-remover") {
		t.Fatalf("closest: %s", out)
	}
	if _, out, _ := run(t, "search", "remove", "background"); !strings.Contains(out, "MATCHED") || !strings.Contains(out, "remove,background") {
		t.Fatalf("matched column: %s", out)
	}
}

func TestSearchShowsTestStatus(t *testing.T) {
	isolate(t)
	newToolbox(t, "main")
	run(t, "new", "text-tool", "--lang", "sh")
	run(t, "import", "--yes", filepath.Join(os.Getenv("MAGGOT_DATA_DIR"), "drafts", "text-tool"))
	run(t, "test", "text-tool")
	_, out, _ := run(t, "search", "text", "files", "--json")
	var res struct {
		Results []struct {
			Ref      string
			LastTest *struct {
				OK    bool
				Cases []struct{ Name, Status string }
			} `json:"last_test"`
		}
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Results) == 0 || res.Results[0].LastTest == nil || !res.Results[0].LastTest.OK || len(res.Results[0].LastTest.Cases) != 3 {
		t.Fatalf("search should carry the test record: %s", out)
	}
}
