package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/nxnom/maggot/internal/config"
	"github.com/spf13/cobra"
)

func (a *app) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Open maggot's config file in your editor (same as `config edit`)",
		Long: `maggot's own settings: the registered toolboxes and the default toolbox.
Tool and toolbox metadata live in tool.json / maggot.json, not here.

Location: $MAGGOT_CONFIG_DIR, $XDG_CONFIG_HOME/maggot, ~/.config/maggot
(%AppData%\maggot on Windows). Editor: $VISUAL, $EDITOR, then vi (notepad on Windows).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return a.editConfig() },
	}
	cmd.AddCommand(
		&cobra.Command{
			Use: "edit", Short: "Open the config file in your editor", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error { return a.editConfig() },
		},
		&cobra.Command{
			Use: "path", Short: "Print the config file path", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				p, err := configPath()
				if err == nil {
					fmt.Fprintln(a.stdout, p)
				}
				return err
			},
		},
		&cobra.Command{
			Use: "show", Short: "Print the config file", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				p, err := configPath()
				if err != nil {
					return err
				}
				data, err := os.ReadFile(p)
				if errors.Is(err, fs.ErrNotExist) {
					fmt.Fprintln(a.stderr, "no config file yet:", p)
					return nil
				}
				if err == nil {
					_, err = a.stdout.Write(data)
				}
				return err
			},
		},
		&cobra.Command{
			Use: "validate", Short: "Check the config file for mistakes", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error { return a.validateConfig() },
		},
	)
	return cmd
}

func configPath() (string, error) {
	c, err := config.Load()
	if err != nil {
		// Still print the path when the file is broken.
		dir, derr := config.Dir()
		if derr != nil {
			return "", derr
		}
		return dir + string(os.PathSeparator) + "config.json", nil
	}
	return c.Path(), nil
}

func (a *app) validateConfig() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	problems := c.Problems()
	if len(problems) == 0 {
		fmt.Fprintln(a.stdout, "config ok:", c.Path())
		return nil
	}
	fmt.Fprintln(a.stdout, "config problems in", c.Path())
	for _, p := range problems {
		fmt.Fprintln(a.stdout, "  -", p)
	}
	return &exitError{code: 1}
}

// editorCommand returns the editor argv: $VISUAL, $EDITOR, or an OS default.
// The variables may contain args, for example "code --wait".
func editorCommand() []string {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if f := strings.Fields(os.Getenv(v)); len(f) > 0 {
			return f
		}
	}
	if runtime.GOOS == "windows" {
		return []string{"notepad"}
	}
	return []string{"vi"}
}

func (a *app) editConfig() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		c, _ := config.LoadFile(p)
		if err := c.Save(); err != nil {
			return err
		}
	}
	argv := append(editorCommand(), p)
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = a.stdin, a.stdout, a.stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor %q: %w", argv[0], err)
	}
	return a.validateConfig()
}
