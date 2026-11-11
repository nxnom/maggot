// Package cli wires maggot's commands together.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/nxnom/maggot/internal/doctor"
	"github.com/nxnom/maggot/internal/secrets"

	"github.com/spf13/cobra"
)

// Version is set at build time with -ldflags "-X github.com/nxnom/maggot/internal/cli.Version=x.y.z".
var Version = "dev"

// exitError lets a command return a specific process exit code (for example
// the exit code of a tool started by `maggot run`).
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	toolboxFlag string

	// Test hooks. Nil means use the real implementation.
	doctor           *doctor.Checker
	installer        func(argv []string) error
	secrets          secrets.Store
	forceInteractive *bool
	reader           *bufio.Reader
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:           "maggot",
		Short:         "Find, run, create and share standalone CLI tools",
		Long:          "maggot manages toolboxes: plain folders of standalone CLI tools described by a tool.json manifest.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
		Args:          cobra.NoArgs,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetIn(a.stdin)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.PersistentFlags().StringVar(&a.toolboxFlag, "toolbox", "", "use only this toolbox (registered name or path); env: MAGGOT_TOOLBOX")
	root.AddCommand(a.toolboxCmd(), a.listCmd(), a.searchCmd(), a.infoCmd(), a.runCmd(), a.doctorCmd(), a.secretCmd(), a.packCmd(), a.exportCmd(), a.importCmd(), a.removeCmd(), a.newCmd(), a.testCmd(), a.guideCmd(), a.configCmd(), a.skillCmd(), a.uiCmd())
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return (&app{stdin: stdin, stdout: stdout, stderr: stderr}).execute(args)
}

func (a *app) execute(args []string) int {
	root := newRootCmd(a)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	fmt.Fprintln(a.stderr, "error:", err)
	return 1
}
