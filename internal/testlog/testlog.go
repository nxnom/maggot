// Package testlog remembers the last test result of each tool, outside the
// toolbox, so maggot can show tools that stopped working.
package testlog

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/nxnom/maggot/internal/config"
)

const fileName = "test-results.json"

// Record is the result of the last `maggot test` of one tool.
type Record struct {
	Version string    `json:"version"`
	OK      bool      `json:"ok"`
	Passed  int       `json:"passed"`
	Failed  int       `json:"failed"`
	Skipped int       `json:"skipped"`
	Error   string    `json:"error,omitempty"` // manifest/requirements/no tests
	At      time.Time `json:"at"`
	// Cases says what was actually tested, so an agent can tell whether the
	// capability it needs was covered.
	Cases []Case `json:"cases,omitempty"`
}

// Case is the result of one test case.
type Case struct {
	Name   string `json:"name"`
	Status string `json:"status"` // passed, failed, skipped
	Reason string `json:"reason,omitempty"`
}

func path() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load returns all records keyed by tool ref ("toolbox/id").
func Load() (map[string]Record, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	out := map[string]Record{}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]Record{}, nil // a broken file must not break maggot
	}
	return out, nil
}

// Save stores the record for ref.
func Save(ref string, r Record) error {
	all, err := Load()
	if err != nil {
		return err
	}
	if r.At.IsZero() {
		r.At = time.Now()
	}
	all[ref] = r
	p, _ := path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(all, "", "  ")
	return config.WriteFileAtomic(p, data, 0o644)
}

// Label is a short status for tables: "ok 3d", "FAIL 2h", "old" (an older
// version was tested) or "-" (never tested).
func Label(r *Record, version string, now time.Time) string {
	if r == nil {
		return "-"
	}
	if r.Version != version {
		return "old"
	}
	s := "ok"
	if !r.OK {
		s = "FAIL"
	}
	return s + " " + Age(now.Sub(r.At))
}

// Age formats a duration as a short age: 5m, 3h, 2d.
func Age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return itoa(int(d.Hours())) + "h"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}

func itoa(n int) string {
	if n < 1 {
		n = 0
	}
	b, _ := json.Marshal(n)
	return string(b)
}
