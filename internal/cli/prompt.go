package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

var errNotConfirmed = errors.New("cancelled")

// interactive reports whether stdin is a terminal.
func (a *app) interactive() bool {
	if a.forceInteractive != nil {
		return *a.forceInteractive
	}
	f, ok := a.stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// confirm asks a yes/no question. With yes=true it returns true without
// asking. Without a terminal it refuses, so agents must pass --yes on purpose
// (after the user agreed).
func (a *app) confirm(yes bool, question string) error {
	if yes {
		return nil
	}
	if !a.interactive() {
		return fmt.Errorf("%s: confirmation needed; re-run with --yes after the user approves", question)
	}
	fmt.Fprintf(a.stderr, "%s [y/N] ", question)
	if a.reader == nil {
		a.reader = bufio.NewReader(a.stdin)
	}
	line, _ := a.reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return errNotConfirmed
}
