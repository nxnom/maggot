package presets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPresets(t *testing.T) {
	t.Setenv("MAGGOT_DATA_DIR", t.TempDir())
	toolDir := t.TempDir()
	os.MkdirAll(filepath.Join(toolDir, "voices"), 0o755)
	ref := filepath.Join(toolDir, "voices", "calm.wav")

	if _, err := Save("tts", toolDir, Preset{Name: "Bad Name", Scope: Personal}); err == nil {
		t.Fatal("invalid name should fail")
	}
	// Toolbox preset: paths in the tool folder become {tool}/..., others warn.
	warnings, err := Save("tts", toolDir, Preset{Name: "narrator", Label: "Narrator", Scope: Toolbox,
		Values: map[string]any{"ref": ref, "lines": "/Users/me/lines.json", "takes": float64(2)}})
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "/Users/me/lines.json") {
		t.Fatalf("save toolbox: %v %v", warnings, err)
	}
	raw, _ := os.ReadFile(filepath.Join(toolDir, Dir, "narrator.json"))
	if !strings.Contains(string(raw), `"{tool}/voices/calm.wav"`) {
		t.Fatalf("stored: %s", raw)
	}
	// Personal preset with the same name wins by default.
	if _, err := Save("tts", toolDir, Preset{Name: "narrator", Scope: Personal, Values: map[string]any{"takes": float64(5)}}); err != nil {
		t.Fatal(err)
	}
	p, err := Get("tts", toolDir, "narrator")
	if err != nil || p.Scope != Personal || p.Values["takes"] != float64(5) {
		t.Fatalf("default: %+v %v", p, err)
	}
	p, err = Get("tts", toolDir, "toolbox/narrator")
	if err != nil || p.Scope != Toolbox || p.Values["ref"] != ref || p.Label != "Narrator" {
		t.Fatalf("toolbox: %+v %v", p, err)
	}
	if list := List("tts", toolDir); len(list) != 2 || list[0].Scope != Personal || list[1].Scope != Toolbox {
		t.Fatalf("list: %+v", list)
	}
	merged := Merge(p, map[string]any{"takes": float64(1), "text": "hi"})
	if merged["takes"] != float64(1) || merged["text"] != "hi" || merged["ref"] != ref {
		t.Fatalf("merge: %v", merged)
	}
	if _, err := Get("tts", toolDir, "nope"); err == nil || !strings.Contains(err.Error(), "personal/narrator, toolbox/narrator") {
		t.Fatalf("missing: %v", err)
	}
	if err := Delete("tts", toolDir, "narrator"); err != nil {
		t.Fatal(err)
	}
	if p, _ := Get("tts", toolDir, "narrator"); p == nil || p.Scope != Toolbox {
		t.Fatalf("after deleting personal, toolbox remains: %+v", p)
	}
	if Slug("Narrator (calm, deep)") != "narrator-calm-deep" {
		t.Fatal(Slug("Narrator (calm, deep)"))
	}
}
