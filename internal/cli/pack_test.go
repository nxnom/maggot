package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportImportRoundTrip(t *testing.T) {
	isolate(t)
	src := newToolbox(t, "src")
	addTool(t, src, "hello", helloManifest, helloScript)
	outDir := t.TempDir()

	code, out, e := run(t, "export", "hello", "-o", outDir)
	archive := strings.TrimSpace(out)
	if code != 0 || filepath.Base(archive) != "hello-1.0.0.maggot" {
		t.Fatalf("export: code=%d out=%q err=%s", code, out, e)
	}

	dst := newToolbox(t, "dst")
	yes := true
	interactive := func(a *app) { a.forceInteractive = &yes }

	// Without a terminal, import must ask for --yes.
	if code, _, e := run(t, "--toolbox", "dst", "import", archive); code == 0 || !strings.Contains(e, "--yes") {
		t.Fatalf("should need confirmation: %s", e)
	}
	// Answering "n" on a terminal does not import.
	code, _, e = runApp(t, "n\n", interactive, "--toolbox", "dst", "import", archive)
	if code != 1 || !strings.Contains(e, "This tool is code") || !strings.Contains(e, "not imported") {
		t.Fatalf("decline: code=%d err=%s", code, e)
	}
	// Answering "y" imports.
	code, out, e = runApp(t, "y\n", interactive, "--toolbox", "dst", "import", archive)
	if code != 0 || !strings.Contains(out, "installed dst/hello 1.0.0") {
		t.Fatalf("import: code=%d out=%s err=%s", code, out, e)
	}
	if code, out, _ := run(t, "--toolbox", "dst", "run", "hello", "x"); code != 0 || out != "hello x\n" {
		t.Fatalf("imported tool should run: %q", out)
	}

	// Same version again needs --force.
	code, out, _ = run(t, "--toolbox", "dst", "import", "--yes", "--json", archive)
	var res struct {
		Imported []any
		Errors   []string
	}
	json.Unmarshal([]byte(out), &res)
	if code != 1 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "--force") {
		t.Fatalf("same version: code=%d out=%s", code, out)
	}
	if code, out, _ := run(t, "--toolbox", "dst", "import", "--yes", "--force", archive); code != 0 || !strings.Contains(out, "reinstalled") {
		t.Fatalf("force: %s", out)
	}
	_ = dst
}

func TestImportFolderIntoDefaultAndPack(t *testing.T) {
	isolate(t)
	newToolbox(t, "main") // becomes default
	work := t.TempDir()
	toolDir := filepath.Join(work, "hello")
	os.MkdirAll(toolDir, 0o755)
	os.WriteFile(filepath.Join(toolDir, "tool.json"), []byte(helloManifest), 0o644)
	os.WriteFile(filepath.Join(toolDir, "run"), []byte(helloScript), 0o755)

	if code, out, e := run(t, "pack", toolDir, "-o", filepath.Join(work, "h.maggot")); code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "h.maggot") {
		t.Fatalf("pack: %s %s", out, e)
	}
	if code, out, e := run(t, "import", "--yes", toolDir); code != 0 || !strings.Contains(out, "main/hello") {
		t.Fatalf("import folder: %s %s", out, e)
	}
	if code, _, e := run(t, "remove", "hello"); code == 0 || !strings.Contains(e, "--yes") {
		t.Fatalf("remove should ask: %s", e)
	}
	if code, _, _ := run(t, "remove", "--yes", "hello"); code != 0 {
		t.Fatal("remove failed")
	}
	if code, _, _ := run(t, "info", "hello"); code == 0 {
		t.Fatal("tool should be gone")
	}
}

func TestImportWithoutToolbox(t *testing.T) {
	isolate(t)
	if code, _, e := run(t, "import", "--yes", t.TempDir()); code == 0 || !strings.Contains(e, "no default toolbox") {
		t.Fatalf("want no default toolbox: %s", e)
	}
}
