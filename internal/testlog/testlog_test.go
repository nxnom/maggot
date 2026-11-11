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
