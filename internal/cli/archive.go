package cli

import (
	"fmt"
	"os"

	"github.com/nxnom/maggot/internal/skill"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/spf13/cobra"
)

func (a *app) archiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive <tool|skill>",
		Short: "Hide a tool or skill from search, list and agents, without deleting it",
		Long: `Move a tool or skill to <toolbox>/.archive/. It no longer shows in
'maggot search' or 'maggot list', cannot be run, and a skill is removed from
the agents' skills folders. Bring it back with 'maggot restore'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			if t, terr := e.Set.Resolve(args[0]); terr == nil {
				if err := t.Toolbox.Archive(toolbox.KindTool, t.Manifest.ID); err != nil {
					return err
				}
				fmt.Fprintf(a.stdout, "archived tool %s\n", t.Ref())
				return nil
			} else if sk, serr := e.Set.ResolveSkill(args[0]); serr == nil {
				a.unlinkSkill(sk)
				if err := sk.Toolbox.Archive(toolbox.KindSkill, sk.Meta.Name); err != nil {
					return err
				}
				fmt.Fprintf(a.stdout, "archived skill %s\n", sk.Ref())
				return nil
			} else {
				return terr
			}
		},
	}
}

// unlinkSkill removes a skill's links from the agents' skills folders.
func (a *app) unlinkSkill(sk *toolbox.Skill) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	for _, ag := range skill.Agents() {
		if p, removed, err := skill.Unlink(home, ag, sk.Meta.Name, sk.Dir, false); err == nil && removed {
			fmt.Fprintf(a.stdout, "%s: removed %s\n", ag, p)
		}
	}
}

func (a *app) restoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <name>",
		Short: "Bring an archived tool or skill back",
		Long: `Move an archived tool or skill back into its toolbox. Use 'maggot archived'
to see what is archived. Use <toolbox>/<name> if the name is in more than one
toolbox. A skill is not linked into agents again; run 'maggot skill install <name>'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			var found []toolbox.Archived
			for _, it := range e.Set.Archived() {
				if it.Name == args[0] || it.Ref() == args[0] {
					found = append(found, it)
				}
			}
			if len(found) == 0 {
				return fmt.Errorf("nothing archived as %q (see `maggot archived`)", args[0])
			}
			if len(found) > 1 {
				var refs []string
				for _, it := range found {
					refs = append(refs, it.Kind+" "+it.Ref())
				}
				return fmt.Errorf("%q is ambiguous: %v; use <toolbox>/<name>", args[0], refs)
			}
			it := found[0]
			tb, _ := e.Set.Toolbox(it.Toolbox)
			if err := tb.Restore(it.Kind, it.Name); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "restored %s %s\n", it.Kind, it.Ref())
			return nil
		},
	}
}

func (a *app) archivedCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "archived",
		Short: "List archived tools and skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			items := e.Set.Archived()
			if asJSON {
				if items == nil {
					items = []toolbox.Archived{}
				}
				return writeJSON(a.stdout, items)
			}
			if len(items) == 0 {
				fmt.Fprintln(a.stdout, "nothing archived")
				return nil
			}
			for _, it := range items {
				fmt.Fprintf(a.stdout, "%s\t%s\n", it.Kind, it.Ref())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
