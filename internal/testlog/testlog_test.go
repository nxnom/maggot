package testlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadLabel(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MAGGOT_DATA_DIR", d)
	if all, err := Load(); err != nil || len(all) != 0 {
		t.Fatalf("empty: %v %v", all, err)
	}
	at := time.Now().Add(-3 * time.Hour)
	if err := Save("p/a", Record{Version: "1.0.0", OK: true, Passed: 3, At: at}); err != nil {
		t.Fatal(err)
	}
	Save("p/b", Record{Version: "1.0.0", OK: false, Failed: 1})
	all, _ := Load()
	a, b := all["p/a"], all["p/b"]
	now := time.Now()
	if Label(&a, "1.0.0", now) != "ok 3h" || Label(&b, "1.0.0", now) != "FAIL 0m" || Label(&a, "1.1.0", now) != "old" || Label(nil, "1", now) != "-" {
		t.Fatalf("labels: %q %q %q", Label(&a, "1.0.0", now), Label(&b, "1.0.0", now), Label(&a, "1.1.0", now))
	}
	if Age(50*time.Hour) != "2d" {
		t.Fatal(Age(50 * time.Hour))
	}
	os.WriteFile(filepath.Join(d, fileName), []byte("{broken"), 0o644)
	if all, err := Load(); err != nil || len(all) != 0 {
		t.Fatal("broken file should load as empty")
	}
}

func TestAdoptNeedsSameVersionAndFiles(t *testing.T) {
	t.Setenv("MAGGOT_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "run"), []byte("a"), 0o644)
	os.MkdirAll(filepath.Join(dir, "__pycache__"), 0o755)
	h := DirHash(dir)
	Save(DraftKey("x"), Record{Version: "1.0.0", OK: true, Passed: 2, Hash: h})

	os.WriteFile(filepath.Join(dir, "__pycache__", "x.pyc"), []byte("junk"), 0o644)
	if DirHash(dir) != h {
		t.Fatal("caches must not change the hash")
	}
	if Adopt(DraftKey("x"), "box/x", "1.0.1", h) {
		t.Fatal("different version adopted")
	}
	os.WriteFile(filepath.Join(dir, "run"), []byte("b"), 0o644)
	if Adopt(DraftKey("x"), "box/x", "1.0.0", DirHash(dir)) {
		t.Fatal("changed files adopted")
	}
	os.WriteFile(filepath.Join(dir, "run"), []byte("a"), 0o644)
	if !Adopt(DraftKey("x"), "box/x", "1.0.0", DirHash(dir)) {
		t.Fatal("same version and files not adopted")
	}
	all, _ := Load()
	if _, still := all[DraftKey("x")]; still || all["box/x"].Passed != 2 {
		t.Fatalf("after adopt: %+v", all)
	}
}
