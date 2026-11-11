package cli

import (
	"encoding/json"
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
}
