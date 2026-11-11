// Package secrets stores secret env values for tools outside the toolbox, so
// a toolbox can be committed to git without leaking them.
//
// Values go to the OS keychain (macOS Keychain, Windows Credential Manager,
// Secret Service on Linux). When no keychain is available, or when
// MAGGOT_SECRETS=file, they go to <config dir>/secrets.json with mode 0600.
package secrets

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/nxnom/maggot/internal/config"
	"github.com/zalando/go-keyring"
)

const service = "maggot"

type Store interface {
	Get(tool, name string) (string, bool, error)
	Set(tool, name, value string) error
	Delete(tool, name string) error
}

func key(tool, name string) string { return tool + "/" + name }

// Default returns the keychain store with file fallback, or the file store
// only when MAGGOT_SECRETS=file.
func Default() (Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	file := &FileStore{Path: filepath.Join(dir, "secrets.json")}
	if os.Getenv("MAGGOT_SECRETS") == "file" {
		return file, nil
	}
	return &KeyringStore{Fallback: file}, nil
}

// KeyringStore uses the OS keychain and falls back to a file when the keychain
// cannot be used.
type KeyringStore struct{ Fallback *FileStore }

func (k *KeyringStore) Get(tool, name string) (string, bool, error) {
	v, err := keyring.Get(service, key(tool, name))
	if err == nil {
		return v, true, nil
	}
	// Not in the keychain (or no keychain): the value may be in the fallback file.
	return k.Fallback.Get(tool, name)
}

func (k *KeyringStore) Set(tool, name, value string) error {
	if err := keyring.Set(service, key(tool, name), value); err != nil {
		return k.Fallback.Set(tool, name, value)
	}
	// Remove any older copy from the fallback file.
	_ = k.Fallback.Delete(tool, name)
	return nil
}

func (k *KeyringStore) Delete(tool, name string) error {
	errK := keyring.Delete(service, key(tool, name))
	errF := k.Fallback.Delete(tool, name)
	if errK != nil && errF != nil {
		return ErrNotFound
	}
	return nil
}

var ErrNotFound = errors.New("secret not found")

// FileStore keeps secrets in a JSON file readable only by the user.
type FileStore struct {
	Path string
	mu   sync.Mutex
}

func (f *FileStore) read() (map[string]string, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (f *FileStore) write(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(f.Path, data, 0o600)
}

func (f *FileStore) Get(tool, name string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return "", false, err
	}
	v, ok := m[key(tool, name)]
	return v, ok, nil
}

func (f *FileStore) Set(tool, name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return err
	}
	m[key(tool, name)] = value
	return f.write(m)
}

func (f *FileStore) Delete(tool, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return err
	}
	if _, ok := m[key(tool, name)]; !ok {
		return ErrNotFound
	}
	delete(m, key(tool, name))
	return f.write(m)
}
