package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// examplesToolbox returns the absolute path; call it before isolate() changes
// the working directory.
func examplesToolbox(t *testing.T) string {
	abs, err := filepath.Abs("../../examples/toolbox")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestExampleToolsAreValid(t *testing.T) {
	abs := examplesToolbox(t)
	isolate(t)
	code, out, errOut := run(t, "--toolbox", abs, "list")
	if code != 0 || strings.Contains(errOut, "warning") || !strings.Contains(out, "examples/youtube-download") {
		t.Fatalf("code=%d out=%s err=%s", code, out, errOut)
	}
	code, out, _ = run(t, "--toolbox", abs, "run", "--dry-run", "youtube-download",
		"--inputs", `{"urls":["https://x"],"mode":"mp3","max_height":"720"}`)
	want := "yt-dlp --newline --no-playlist -o '%(title)s.%(ext)s' -x --audio-format mp3 -S res:720 https://x"
	if code != 0 || strings.TrimSpace(out) != want {
		t.Fatalf("dry run:\n got %s\nwant %s", out, want)
	}
}

func TestExampleYouTubeDownload(t *testing.T) {
	for _, bin := range []string{"yt-dlp", "ffmpeg"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	dir := filepath.Join(examplesToolbox(t), "tools", "youtube-download")
	isolate(t)
	code, out, _ := run(t, "test", dir)
	if code != 0 || !strings.Contains(out, "4/4 cases passed") {
		t.Fatalf("code=%d\n%s", code, out)
	}
}

func TestNewNeedsKind(t *testing.T) {
	isolate(t)
	if code, _, e := run(t, "new", "x", "--dir", t.TempDir()); code == 0 || !strings.Contains(e, `"lang" not set`) {
		t.Fatalf("want required --lang: %s", e)
	}
	dir := t.TempDir()
	if code, _, e := run(t, "new", "git-ver", "--lang", "bin", "--bin", "git", "--dir", dir); code != 0 {
		t.Fatal(e)
	}
	if code, out, _ := run(t, "test", filepath.Join(dir, "git-ver")); code != 0 || !strings.Contains(out, "1/1") {
		t.Fatalf("bin tool test: %s", out)
	}
}
