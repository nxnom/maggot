package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/nxnom/maggot/internal/skill"
	"github.com/spf13/cobra"
)

func (a *app) skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the maggot skill for coding agents (Claude Code, Codex, ...)",
	}
	var agents []string
	var project bool
	base := func() (string, error) {
		if project {
			return os.Getwd()
		}
		return os.UserHomeDir()
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Write SKILL.md into each agent's skills folder",
		Long: `Write the single maggot SKILL.md for each agent:
  claude → ~/.claude/skills/maggot/SKILL.md
  codex  → ~/.codex/skills/maggot/SKILL.md
  agents → ~/.agents/skills/maggot/SKILL.md (shared folder some agents read)
With --project the folders are created in the current directory instead.
Run it again after upgrading maggot to update the skill.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := base()
			if err != nil {
				return err
			}
			for _, ag := range agents {
				p, changed, err := skill.Install(b, strings.TrimSpace(ag))
				if err != nil {
					return err
				}
				state := "up to date"
				if changed {
					state = "installed"
				}
				fmt.Fprintf(a.stdout, "%s: %s (%s)\n", ag, p, state)
			}
			return nil
		},
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the maggot skill folder for each agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := base()
			if err != nil {
				return err
			}
			for _, ag := range agents {
				p, err := skill.Uninstall(b, strings.TrimSpace(ag))
				if err != nil {
					return err
				}
				fmt.Fprintf(a.stdout, "%s: removed %s\n", ag, p)
			}
			return nil
		},
	}
	for _, c := range []*cobra.Command{install, uninstall} {
		c.Flags().StringSliceVar(&agents, "agent", []string{"claude", "codex"}, "agents: "+strings.Join(skill.Agents(), ", "))
		c.Flags().BoolVar(&project, "project", false, "use the current directory instead of the home folder")
	}
	show := &cobra.Command{
		Use:   "show",
		Short: "Print SKILL.md",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(a.stdout, skill.Text)
			return err
		},
	}
	cmd.AddCommand(install, uninstall, show)
	return cmd
}
