package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestFileStore(t *testing.T) {
	f := &FileStore{Path: filepath.Join(t.TempDir(), "x", "secrets.json")}
	if _, ok, err := f.Get("tool", "KEY"); ok || err != nil {
		t.Fatalf("empty get: %v %v", ok, err)
	}
	if err := f.Set("tool", "KEY", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := f.Get("tool", "KEY"); !ok || v != "s3cret" {
		t.Fatalf("get: %q %v", v, ok)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(f.Path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode().Perm())
		}
	}
	if err := f.Delete("tool", "KEY"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("tool", "KEY"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestKeyringStore(t *testing.T) {
	keyring.MockInit()
	fb := &FileStore{Path: filepath.Join(t.TempDir(), "secrets.json")}
	k := &KeyringStore{Fallback: fb}

	// A value only in the fallback file is still found.
	fb.Set("tool", "OLD", "from-file")
	if v, ok, _ := k.Get("tool", "OLD"); !ok || v != "from-file" {
		t.Fatalf("fallback get: %q %v", v, ok)
	}
	// Set goes to the keychain and clears the file copy.
	if err := k.Set("tool", "OLD", "from-keyring"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := fb.Get("tool", "OLD"); ok {
		t.Fatal("file copy should be removed")
	}
	if v, _, _ := k.Get("tool", "OLD"); v != "from-keyring" {
		t.Fatalf("got %q", v)
	}
	if err := k.Delete("tool", "OLD"); err != nil {
		t.Fatal(err)
	}
	if err := k.Delete("tool", "OLD"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestKeyringFailureFallsBackToFile(t *testing.T) {
	keyring.MockInitWithError(errors.New("no dbus"))
	fb := &FileStore{Path: filepath.Join(t.TempDir(), "secrets.json")}
	k := &KeyringStore{Fallback: fb}
	if err := k.Set("tool", "KEY", "v"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := fb.Get("tool", "KEY"); !ok || v != "v" {
		t.Fatal("value should be in the file")
	}
	if v, ok, _ := k.Get("tool", "KEY"); !ok || v != "v" {
		t.Fatal("get should read the file")
	}
}
