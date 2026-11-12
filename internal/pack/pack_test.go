package pack

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeTool(t *testing.T, dir, id, version string) {
	t.Helper()
	files := map[string]string{
		"tool.json":           fmt.Sprintf(`{"schema":1,"id":%q,"version":%q,"name":"T","description":"d","entry":{"path":"bin/run"}}`, id, version),
		"bin/run":             "#!/bin/sh\necho hi\n",
		"lib/data.txt":        "data",
		".git/HEAD":           "ref",
		"node_modules/x/a.js": "x",
		"cache/tmp.bin":       "junk",
		".maggotignore":       "# comment\ncache/\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		mode := os.FileMode(0o644)
		if name == "bin/run" {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
}

func entries(t *testing.T, archive string) map[string]os.FileMode {
	t.Helper()
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string]os.FileMode{}
	for _, f := range zr.File {
		out[f.Name] = f.Mode()
	}
	return out
}

func TestPackContents(t *testing.T) {
	src := t.TempDir()
	writeTool(t, src, "demo", "1.0.0")
	out := filepath.Join(t.TempDir(), "demo.maggot")
	m, err := Pack(src, out)
	if err != nil {
		t.Fatal(err)
	}
	if FileName(m) != "demo-1.0.0.maggot" {
		t.Fatalf("name %s", FileName(m))
	}
	got := entries(t, out)
	for _, want := range []string{"tool.json", "bin/run", "lib/data.txt", "node_modules/x/a.js"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	for _, skip := range []string{".git/HEAD", "cache/tmp.bin", ".maggotignore"} {
		if _, ok := got[skip]; ok {
			t.Errorf("%s should be ignored", skip)
		}
	}
	if got["bin/run"].Perm() != 0o755 || got["lib/data.txt"].Perm() != 0o644 {
		t.Errorf("modes: %v", got)
	}
	// Reproducible: packing twice gives identical bytes.
	out2 := filepath.Join(t.TempDir(), "demo.maggot")
	Pack(src, out2)
	a, _ := os.ReadFile(out)
	b, _ := os.ReadFile(out2)
	if string(a) != string(b) {
		t.Error("archives differ")
	}
}

func TestPackRejectsSymlinkAndInvalid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	src := t.TempDir()
	writeTool(t, src, "demo", "1.0.0")
	os.Symlink("/etc/passwd", filepath.Join(src, "lib", "link"))
	if _, err := Pack(src, filepath.Join(t.TempDir(), "x.maggot")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("want symlink error, got %v", err)
	}
	if _, err := Pack(t.TempDir(), filepath.Join(t.TempDir(), "x.maggot")); err == nil {
		t.Fatal("folder without tool.json should fail")
	}
}

func TestImportFlow(t *testing.T) {
	tools := filepath.Join(t.TempDir(), "tools")
	pack := func(version string) string {
		src := t.TempDir()
		writeTool(t, src, "demo", version)
		out := filepath.Join(t.TempDir(), "demo.maggot")
		if _, err := Pack(src, out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	s, err := Stage(pack("1.0.0"), tools)
	if err != nil {
		t.Fatal(err)
	}
	if s.Action != "install" || s.NeedsForce() || s.Files != 4 {
		t.Fatalf("preview: %+v", s.Preview)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(tools, "demo", "bin", "run"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o755) {
		t.Fatalf("exec bit lost: %v %v", info, err)
	}

	s, _ = Stage(pack("1.1.0"), tools)
	if s.Action != "upgrade" || s.Existing != "1.0.0" || s.NeedsForce() {
		t.Fatalf("upgrade: %+v", s.Preview)
	}
	s.Commit()

	s, _ = Stage(pack("1.0.0"), tools)
	if s.Action != "downgrade" || !s.NeedsForce() {
		t.Fatalf("downgrade: %+v", s.Preview)
	}
	s.Discard()
	s, _ = Stage(pack("1.1.0"), tools)
	if s.Action != "reinstall" || !s.NeedsForce() {
		t.Fatalf("reinstall: %+v", s.Preview)
	}
	s.Discard()

	// No leftovers from staging.
	left, _ := os.ReadDir(tools)
	if len(left) != 1 || left[0].Name() != "demo" {
		t.Fatalf("leftovers: %v", left)
	}
}

func TestImportFolder(t *testing.T) {
	src := t.TempDir()
	writeTool(t, src, "demo", "1.0.0")
	tools := t.TempDir()
	s, err := Stage(src, tools)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tools, "demo", ".git")); err == nil {
		t.Fatal(".git should not be copied")
	}
}

// makeZip writes a zip with the given entries (name → body; mode 0755 for "run").
func makeZip(t *testing.T, files map[string]string, symlink string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, "run") {
			mode = 0o755
		}
		h.SetMode(mode)
		w, _ := zw.CreateHeader(h)
		w.Write([]byte(body))
	}
	if symlink != "" {
		h := &zip.FileHeader{Name: symlink}
		h.SetMode(os.ModeSymlink | 0o777)
		w, _ := zw.CreateHeader(h)
		w.Write([]byte("/etc/passwd"))
	}
	zw.Close()
	f.Close()
	return p
}

const okManifest = `{"schema":1,"id":"z","version":"1.0.0","name":"Z","description":"d","entry":{"path":"run"}}`

func TestImportZipShapes(t *testing.T) {
	// Folder zipped with its top folder, plus macOS junk.
	src := makeZip(t, map[string]string{"z/tool.json": okManifest, "z/run": "#!/bin/sh\n", "__MACOSX/z/._run": "x"}, "")
	s, err := Stage(src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.Manifest.ID != "z" || s.Files != 2 {
		t.Fatalf("nested: %+v", s.Preview)
	}
	s.Discard()
}

func TestImportRejectsUnsafeArchives(t *testing.T) {
	cases := map[string]string{
		"zip slip":  makeZip(t, map[string]string{"tool.json": okManifest, "run": "x", "../evil": "x"}, ""),
		"absolute":  makeZip(t, map[string]string{"tool.json": okManifest, "run": "x", "/etc/evil": "x"}, ""),
		"backslash": makeZip(t, map[string]string{"tool.json": okManifest, "run": "x", `a\..\..\evil`: "x"}, ""),
		"symlink":   makeZip(t, map[string]string{"tool.json": okManifest, "run": "x"}, "link"),
		"invalid":   makeZip(t, map[string]string{"tool.json": `{"schema":1}`}, ""),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			tools := t.TempDir()
			if _, err := Stage(src, tools); err == nil {
				t.Fatal("want error")
			}
			if left, _ := os.ReadDir(tools); len(left) != 0 {
				t.Fatalf("staging not cleaned: %v", left)
			}
		})
	}
	if _, err := Stage(filepath.Join(t.TempDir(), "nope.maggot"), t.TempDir()); err == nil {
		t.Fatal("missing file should fail")
	}
}

func TestImportSizeLimit(t *testing.T) {
	old := MaxTotalBytes
	MaxTotalBytes = 10
	defer func() { MaxTotalBytes = old }()
	src := makeZip(t, map[string]string{"tool.json": okManifest, "run": strings.Repeat("x", 100)}, "")
	if _, err := Stage(src, t.TempDir()); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want size error, got %v", err)
	}
}

func TestIgnored(t *testing.T) {
	p := []string{"node_modules", "build/*.o", "*.log"}
	for rel, want := range map[string]bool{
		"node_modules":     true,
		"a/node_modules/x": true,
		"build/a.o":        true,
		"src/build/a.o":    false,
		"x/debug.log":      true,
		"main.go":          false,
	} {
		if ignored(rel, p) != want {
			t.Errorf("ignored(%q) = %v", rel, !want)
		}
	}
}

func writeSkill(t *testing.T, dir, name, version string) {
	t.Helper()
	files := map[string]string{
		"SKILL.md":          fmt.Sprintf("---\nname: %s\ndescription: Do it.\nmetadata:\n  version: %q\n---\nsteps\n", name, version),
		"scripts/run.sh":    "#!/bin/sh\necho hi\n",
		"assets/ref.txt":    "ref",
		".git/HEAD":         "ref",
		"__pycache__/x.pyc": "junk",
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSkillPackAndImport(t *testing.T) {
	src := filepath.Join(t.TempDir(), "demo")
	writeSkill(t, src, "demo", "1.0.0")
	if KindOf(src) != KindSkill {
		t.Fatal("kind")
	}
	out := filepath.Join(t.TempDir(), "demo.maggot")
	sk, err := PackSkill(src, out)
	if err != nil || SkillFileName(sk) != "demo-1.0.0.maggot" {
		t.Fatalf("pack: %v", err)
	}
	zr, _ := zip.OpenReader(out)
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	zr.Close()
	if strings.Join(names, ",") != "SKILL.md,assets/ref.txt,scripts/run.sh" {
		t.Fatalf("files: %v", names)
	}

	box := t.TempDir()
	tools := filepath.Join(box, "tools")
	s, err := Stage(out, tools)
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != KindSkill || s.ID != "demo" || s.Version != "1.0.0" || s.Action != "install" || s.Target != filepath.Join(box, "skills", "demo") {
		t.Fatalf("preview: %+v", s.Preview)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(box, "skills", "demo", "scripts", "run.sh")); err != nil {
		t.Fatal(err)
	}
	// The same version again needs force; a folder source works too.
	s, _ = Stage(src, tools)
	if s.Action != "reinstall" || !s.NeedsForce() {
		t.Fatalf("again: %+v", s.Preview)
	}
	s.Discard()
	if entries, _ := os.ReadDir(tools); len(entries) != 0 {
		t.Fatalf("temp folders left: %v", entries)
	}
}

func TestStageNeitherToolNorSkill(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "README.md"), []byte("x"), 0o644)
	if _, err := Stage(src, filepath.Join(t.TempDir(), "tools")); err == nil || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("got %v", err)
	}
}
