package cli

import (
	"fmt"

	"github.com/nxnom/maggot/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) openCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <tool|folder>",
		Short: "Open a tool's folder in the file manager",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := a.toolDirArg(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, dir)
			return ui.OpenBrowser(dir)
		},
	}
}
