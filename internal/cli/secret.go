package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nxnom/maggot/internal/secrets"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/workspace"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type envStatus = workspace.EnvStatus

func (a *app) secretStore() (secrets.Store, error) {
	if a.secrets != nil {
		return a.secrets, nil
	}
	return secrets.Default()
}

func (a *app) envStatus(t *toolbox.Tool) []envStatus {
	store, _ := a.secretStore()
	return workspace.Env(store, t)
}

func (a *app) secretEnv(t *toolbox.Tool) ([]string, error) {
	store, err := a.secretStore()
	if err != nil {
		return nil, err
	}
	return workspace.SecretEnv(store, t)
}

func (a *app) secretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Store secret env values for tools and skills (kept outside the toolbox)",
		Long: `Store secret env values for tools and skills, outside the toolbox (in the
system keychain). Name a tool, or a skill with --skill. A name that is not
a tool but is a skill works without --skill.`,
	}
	cmd.AddCommand(a.secretSetCmd(), a.secretRmCmd(), a.secretListCmd())
	return cmd
}

// secretOwner is the tool or skill a secret belongs to.
type secretOwner struct {
	key   string // store key
	label string // for messages
	tool  *toolbox.Tool
	skill *toolbox.Skill
}

// owner resolves a tool, or a skill with --skill (or when no tool has that name).
func (a *app) owner(ref string, isSkill bool) (*secretOwner, error) {
	e, err := a.loadEnv()
	if err != nil {
		return nil, err
	}
	if !isSkill {
		t, err := e.Set.Resolve(ref)
		if err == nil {
			return &secretOwner{key: t.Manifest.ID, label: t.Ref(), tool: t}, nil
		}
		if _, skErr := e.Set.ResolveSkill(ref); skErr != nil {
			return nil, err
		}
	}
	sk, err := a.skillArg(e, ref)
	if err != nil {
		return nil, err
	}
	return &secretOwner{key: workspace.SkillSecretKey(sk.Meta.Name), label: "skill " + sk.Ref(), skill: sk}, nil
}

func (o *secretOwner) env(store secrets.Store) []envStatus {
	if o.skill != nil {
		return workspace.SkillEnv(store, o.skill)
	}
	return workspace.Env(store, o.tool)
}

// declaredSecret resolves the owner and checks NAME is declared as a secret.
func (a *app) declaredSecret(ref, name string, isSkill bool) (*secretOwner, error) {
	o, err := a.owner(ref, isSkill)
	if err != nil {
		return nil, err
	}
	if o.skill != nil {
		for _, v := range o.skill.Env {
			if v == name {
				return o, nil
			}
		}
		return nil, fmt.Errorf("%s does not declare env %s (add it to metadata.maggot-env in SKILL.md)", o.label, name)
	}
	for _, v := range o.tool.Manifest.Env {
		if v.Name == name {
			if !v.Secret {
				return nil, fmt.Errorf("%s is not marked secret in %s; set it in the environment instead", name, o.label)
			}
			return o, nil
		}
	}
	return nil, fmt.Errorf("%s does not declare env %s", o.label, name)
}

func (a *app) secretSetCmd() *cobra.Command {
	var isSkill bool
	c := &cobra.Command{
		Use:   "set <tool|skill> <NAME>",
		Short: "Save a secret (value is read from stdin, hidden on a terminal)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := a.declaredSecret(args[0], args[1], isSkill)
			if err != nil {
				return err
			}
			value, err := a.readSecret(args[1])
			if err != nil {
				return err
			}
			if value == "" {
				return fmt.Errorf("empty value")
			}
			store, err := a.secretStore()
			if err != nil {
				return err
			}
			if err := store.Set(o.key, args[1], value); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "saved %s for %s\n", args[1], o.label)
			return nil
		},
	}
	c.Flags().BoolVar(&isSkill, "skill", false, "the name is a skill, not a tool")
	return c
}

func (a *app) readSecret(name string) (string, error) {
	if f, ok := a.stdin.(*os.File); ok && a.interactive() {
		fmt.Fprintf(a.stderr, "%s: ", name)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(a.stderr)
		return string(b), err
	}
	b, err := io.ReadAll(bufio.NewReader(a.stdin))
	return strings.TrimRight(string(b), "\r\n"), err
}

func (a *app) secretRmCmd() *cobra.Command {
	var isSkill bool
	c := &cobra.Command{
		Use:   "rm <tool|skill> <NAME>",
		Short: "Delete a saved secret",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := a.declaredSecret(args[0], args[1], isSkill)
			if err != nil {
				return err
			}
			store, err := a.secretStore()
			if err != nil {
				return err
			}
			return store.Delete(o.key, args[1])
		},
	}
	c.Flags().BoolVar(&isSkill, "skill", false, "the name is a skill, not a tool")
	return c
}

func (a *app) secretListCmd() *cobra.Command {
	var asJSON, isSkill bool
	cmd := &cobra.Command{
		Use:   "list <tool|skill>",
		Short: "Show which env vars of a tool or skill have a value (never prints values)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := a.owner(args[0], isSkill)
			if err != nil {
				return err
			}
			store, _ := a.secretStore()
			st := o.env(store)
			if asJSON {
				ref := ""
				if o.tool != nil {
					ref = o.tool.Ref()
				} else {
					ref = o.skill.Ref()
				}
				return writeJSON(a.stdout, map[string]any{"ref": ref, "env": st})
			}
			for _, s := range st {
				state := "not set"
				if s.Set {
					state = "set (" + s.Source + ")"
				}
				fmt.Fprintf(a.stdout, "%s\t%s\n", s.Name, state)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&isSkill, "skill", false, "the name is a skill, not a tool")
	return cmd
}
