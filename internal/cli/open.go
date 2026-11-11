package cli

import (
	"fmt"

	"github.com/nxnom/maggot/internal/editor"
	"github.com/nxnom/maggot/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) openCmd() *cobra.Command {
	var folder bool
	cmd := &cobra.Command{
		Use:   "open <tool|folder>",
		Short: "Open a tool's folder in your code editor (or the file manager)",
		Long: `Open a tool's folder in your code editor. The editor comes from "editor" in
maggot's config.json (for example "code" or "cursor -n"); when it is not set,
Cursor, VS Code, Zed, Sublime Text or Windsurf is used if installed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := a.toolDirArg(args[0])
			if err != nil {
				return err
			}
			if folder {
				return ui.OpenBrowser(dir)
			}
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			ed, err := editor.Find(e.Config.Editor, nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "opening %s in %s\n", dir, ed.Name)
			return ed.Open(dir)
		},
	}
	cmd.Flags().BoolVar(&folder, "folder", false, "open the folder in the file manager instead")
	return cmd
}
