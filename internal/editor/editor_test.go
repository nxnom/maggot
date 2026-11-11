package editor

import (
	"errors"
	"testing"
)

func TestFind(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("no") }
	only := func(bin string) func(string) (string, error) {
		return func(b string) (string, error) {
			if b == bin {
				return "/bin/" + b, nil
			}
			return "", errors.New("no")
		}
	}
	if e, err := Find("code -n", none); err != nil || e.Name != "code" || len(e.Argv) != 2 {
		t.Fatalf("configured: %+v %v", e, err)
	}
	if e, _ := Find("", only("zed")); e.Name != "Zed" || e.Argv[0] != "zed" {
		t.Fatalf("detected: %+v", e)
	}
	if e, _ := Find("", only("cursor")); e.Name != "Cursor" {
		t.Fatalf("order: %+v", e)
	}
}
