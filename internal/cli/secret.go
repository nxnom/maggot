package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nxnom/maggot/internal/secrets"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type envStatus struct {
	Name     string `json:"name"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
	Set      bool   `json:"set"`
	// Source is "env" (already in the environment), "store" (saved secret) or "".
	Source string `json:"source,omitempty"`
}

func (a *app) secretStore() (secrets.Store, error) {
	if a.secrets != nil {
		return a.secrets, nil
	}
	return secrets.Default()
}

// envStatus reports, for every env var the tool declares, whether a value is
// available. A value in the environment wins over a stored secret.
func (a *app) envStatus(t *toolbox.Tool) []envStatus {
	store, _ := a.secretStore()
	var out []envStatus
	for _, v := range t.Manifest.Env {
		s := envStatus{Name: v.Name, Secret: v.Secret, Required: v.Required}
		if _, ok := os.LookupEnv(v.Name); ok {
			s.Set, s.Source = true, "env"
		} else if v.Secret && store != nil {
			if _, ok, _ := store.Get(t.Manifest.ID, v.Name); ok {
				s.Set, s.Source = true, "store"
			}
		}
		out = append(out, s)
	}
	return out
}

// secretEnv returns NAME=value pairs for stored secrets that are not already
// set in the environment.
func (a *app) secretEnv(t *toolbox.Tool) ([]string, error) {
	var out []string
	var store secrets.Store
	for _, v := range t.Manifest.Env {
		if !v.Secret {
			continue
		}
		if _, ok := os.LookupEnv(v.Name); ok {
			continue
		}
		if store == nil {
			var err error
			if store, err = a.secretStore(); err != nil {
				return nil, err
			}
		}
		val, ok, err := store.Get(t.Manifest.ID, v.Name)
		if err != nil {
			return nil, fmt.Errorf("read secret %s: %w", v.Name, err)
		}
		if ok {
			out = append(out, v.Name+"="+val)
		}
	}
	return out, nil
}

func (a *app) secretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Store secret env values for tools (kept outside the toolbox)",
	}
	cmd.AddCommand(a.secretSetCmd(), a.secretRmCmd(), a.secretListCmd())
	return cmd
}

// declaredSecret resolves the tool and checks NAME is declared as a secret.
func (a *app) declaredSecret(ref, name string) (*toolbox.Tool, error) {
	e, err := a.loadEnv()
	if err != nil {
		return nil, err
	}
	t, err := e.set.Resolve(ref)
	if err != nil {
		return nil, err
	}
	for _, v := range t.Manifest.Env {
		if v.Name == name {
			if !v.Secret {
				return nil, fmt.Errorf("%s is not marked secret in %s; set it in the environment instead", name, t.Ref())
			}
			return t, nil
		}
	}
	return nil, fmt.Errorf("%s does not declare env %s", t.Ref(), name)
}

func (a *app) secretSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <tool> <NAME>",
		Short: "Save a secret (value is read from stdin, hidden on a terminal)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := a.declaredSecret(args[0], args[1])
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
			if err := store.Set(t.Manifest.ID, args[1], value); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "saved %s for %s\n", args[1], t.Manifest.ID)
			return nil
		},
	}
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
	return &cobra.Command{
		Use:   "rm <tool> <NAME>",
		Short: "Delete a saved secret",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := a.declaredSecret(args[0], args[1])
			if err != nil {
				return err
			}
			store, err := a.secretStore()
			if err != nil {
				return err
			}
			return store.Delete(t.Manifest.ID, args[1])
		},
	}
}

func (a *app) secretListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list <tool>",
		Short: "Show which env vars of a tool have a value (never prints values)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			t, err := e.set.Resolve(args[0])
			if err != nil {
				return err
			}
			st := a.envStatus(t)
			if asJSON {
				if st == nil {
					st = []envStatus{}
				}
				return writeJSON(a.stdout, map[string]any{"ref": t.Ref(), "env": st})
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
	return cmd
}
