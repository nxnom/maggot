package doctor

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/nxnom/maggot/internal/manifest"
)

// fake builds a Checker with a fixed set of binaries and their --version output.
func fake(goos string, bins map[string]string) *Checker {
	return &Checker{
		GOOS: goos,
		LookPath: func(name string) (string, error) {
			if _, ok := bins[name]; ok {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		Output: func(_ context.Context, name string, args ...string) (string, error) {
			for bin, out := range bins {
				if strings.HasSuffix(name, "/"+bin) {
					return out, nil
				}
			}
			return "", errors.New("not found")
		},
	}
}

func TestExtractVersion(t *testing.T) {
	cases := map[string]string{
		"ffmpeg version 7.1.1-tessus Copyright": "7.1.1",
		"Version: ImageMagick 7.1.1-43 Q16":     "7.1.1",
		"uv 0.5.11 (Homebrew 2024-12-20)":       "0.5.11",
		"go version go1.27.4 darwin/arm64":      "1.27.4",
		"jq-1.7":                                "1.7.0",
	}
	for in, want := range cases {
		v, ok := ExtractVersion(in)
		if !ok || v.String() != want {
			t.Errorf("ExtractVersion(%q) = %v, want %s", in, v, want)
		}
	}
	if _, ok := ExtractVersion("no version here"); ok {
		t.Error("expected no version")
	}
}

func TestCheckStatuses(t *testing.T) {
	c := fake("darwin", map[string]string{
		"magick": "Version: ImageMagick 6.9.12",
		"jq":     "jq-1.7.1",
		"weird":  "hello",
		"brew":   "Homebrew 4.4",
	})
	ctx := context.Background()

	if r := c.Check(ctx, manifest.Requirement{Bin: "jq"}); r.Status != StatusOK || r.Version != "" {
		t.Errorf("no constraint: %+v", r)
	}
	if r := c.Check(ctx, manifest.Requirement{Bin: "jq", Version: ">=1.7"}); !r.OK() || r.Version != "1.7.1" {
		t.Errorf("ok: %+v", r)
	}
	r := c.Check(ctx, manifest.Requirement{Bin: "magick", Version: ">=7", Install: map[string]string{"brew": "imagemagick"}})
	if r.Status != StatusOutdated || r.Install == nil || r.Install.String() != "brew install imagemagick" {
		t.Errorf("outdated: %+v", r)
	}
	if r := c.Check(ctx, manifest.Requirement{Bin: "weird", Version: ">=1"}); r.Status != StatusUnknown {
		t.Errorf("unknown: %+v", r)
	}
	r = c.Check(ctx, manifest.Requirement{Bin: "ffmpeg", Install: map[string]string{"brew": "ffmpeg", "apt": "ffmpeg"}})
	if r.Status != StatusMissing || r.Install.Manager != "brew" || r.Alternatives["apt"] != "sudo apt-get install -y ffmpeg" {
		t.Errorf("missing: %+v", r)
	}
}

func TestInstallPicksAvailableManager(t *testing.T) {
	req := manifest.Requirement{Bin: "ffmpeg", Install: map[string]string{"brew": "ffmpeg", "apt": "ffmpeg", "winget": "Gyan.FFmpeg"}}
	ctx := context.Background()

	// Linux with apt-get and sudo
	r := fake("linux", map[string]string{"apt-get": "", "sudo": ""}).Check(ctx, req)
	if r.Install == nil || r.Install.String() != "sudo apt-get install -y ffmpeg" {
		t.Errorf("linux: %+v", r.Install)
	}
	// Linux container without sudo
	r = fake("linux", map[string]string{"apt-get": ""}).Check(ctx, req)
	if r.Install == nil || r.Install.String() != "apt-get install -y ffmpeg" {
		t.Errorf("no sudo: %+v", r.Install)
	}
	// Windows
	r = fake("windows", map[string]string{"winget": ""}).Check(ctx, req)
	if r.Install == nil || r.Install.String() != "winget install --exact --id Gyan.FFmpeg" {
		t.Errorf("windows: %+v", r.Install)
	}
	// No usable manager: no command, but alternatives listed
	r = fake("darwin", map[string]string{}).Check(ctx, req)
	if r.Install != nil || len(r.Alternatives) != 3 {
		t.Errorf("none: %+v", r)
	}
	// Language package manager as fallback
	r = fake("darwin", map[string]string{"npm": ""}).Check(ctx, manifest.Requirement{Bin: "tsx", Install: map[string]string{"npm": "tsx"}})
	if r.Install == nil || r.Install.String() != "npm install -g tsx" {
		t.Errorf("npm: %+v", r.Install)
	}
}

func TestRequirementsAddsInterpreter(t *testing.T) {
	m := &manifest.Manifest{Entry: manifest.Entry{Path: "run.py", Interpreter: []string{"uv", "run", "--script"}},
		Requires: []manifest.Requirement{{Bin: "magick"}}}
	reqs := Requirements(m)
	if len(reqs) != 2 || reqs[0].Bin != "uv" {
		t.Fatalf("reqs: %+v", reqs)
	}
	m.Requires = append(m.Requires, manifest.Requirement{Bin: "uv", Version: ">=0.5"})
	if reqs := Requirements(m); len(reqs) != 2 {
		t.Fatalf("should not duplicate: %+v", reqs)
	}
	m.Entry.Interpreter = []string{"/usr/bin/python3"}
	if reqs := Requirements(m); len(reqs) != 2 {
		t.Fatalf("absolute interpreter is not a PATH requirement: %+v", reqs)
	}
}

func TestRequirementsAddsBin(t *testing.T) {
	m := &manifest.Manifest{Entry: manifest.Entry{Bin: "yt-dlp"}}
	if reqs := Requirements(m); len(reqs) != 1 || reqs[0].Bin != "yt-dlp" {
		t.Fatalf("reqs: %+v", reqs)
	}
	m.Requires = []manifest.Requirement{{Bin: "yt-dlp", Version: ">=2024.1"}}
	if reqs := Requirements(m); len(reqs) != 1 || reqs[0].Version == "" {
		t.Fatalf("listed bin should not be duplicated: %+v", reqs)
	}
}
