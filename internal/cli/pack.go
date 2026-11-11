package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/spf13/cobra"
)

// archivePath picks the output file: -o as a file, -o as a dir, or the cwd.
func archivePath(m *manifest.Manifest, out string) (string, error) {
	if out == "" {
		return pack.FileName(m), nil
	}
	if info, err := os.Stat(out); err == nil && info.IsDir() {
		return filepath.Join(out, pack.FileName(m)), nil
	}
	return out, nil
}

func (a *app) packDir(dir, out string) error {
	m, err := manifest.Load(dir)
	if err != nil {
		return err
	}
	dest, err := archivePath(m, out)
	if err != nil {
		return err
	}
	if _, err := pack.Pack(dir, dest); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, dest)
	return nil
}

func (a *app) packCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "pack <tool-dir>",
		Short: "Pack a tool folder into a .maggot archive",
		Long:  "Pack a tool folder into a .maggot archive (a zip). .git, .DS_Store, __pycache__ and paths listed in .maggotignore are left out.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.packDir(args[0], out)
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output file or folder (default: <id>-<version>.maggot)")
	return cmd
}

func (a *app) exportCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export <tool>",
		Short: "Export an installed tool to a .maggot archive",
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
			return a.packDir(t.Dir, out)
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output file or folder (default: <id>-<version>.maggot)")
	return cmd
}

func (a *app) importCmd() *cobra.Command {
	var force, yes, asJSON bool
	cmd := &cobra.Command{
		Use:   "import <file.maggot|file.zip|folder>...",
		Short: "Install tools into a toolbox from archives or folders",
		Long: `Install tools into the default toolbox (or --toolbox). Each source is
validated in a temp folder first. Imported tools are code that will run on
this machine, so maggot shows what will be installed and asks first; pass
--yes only after the user approved.

Installing an older or the same version over an existing tool needs --force.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			tb, err := a.targetToolbox(e)
			if err != nil {
				return err
			}
			var results []pack.Preview
			var failed []string
			for _, src := range args {
				s, err := pack.Stage(src, tb.ToolsPath())
				if err != nil {
					failed = append(failed, fmt.Sprintf("%s: %v", src, err))
					continue
				}
				if s.NeedsForce() && !force {
					s.Discard()
					failed = append(failed, fmt.Sprintf("%s: %s %s is installed and this is %s; use --force to %s",
						src, s.Manifest.ID, s.Existing, s.Manifest.Version, s.Action))
					continue
				}
				if err := a.confirm(yes, a.describeImport(s, tb.Handle)); err != nil {
					s.Discard()
					if errors.Is(err, errNotConfirmed) {
						failed = append(failed, src+": not imported")
						continue
					}
					return err
				}
				if err := s.Commit(); err != nil {
					s.Discard()
					failed = append(failed, fmt.Sprintf("%s: %v", src, err))
					continue
				}
				results = append(results, s.Preview)
				if !asJSON {
					fmt.Fprintf(a.stdout, "%s %s/%s %s\n", pastTense(s.Action), tb.Handle, s.Manifest.ID, s.Manifest.Version)
				}
			}
			if asJSON {
				if results == nil {
					results = []pack.Preview{}
				}
				if failed == nil {
					failed = []string{}
				}
				if err := writeJSON(a.stdout, map[string]any{"imported": results, "errors": failed}); err != nil {
					return err
				}
			} else {
				for _, f := range failed {
					fmt.Fprintln(a.stderr, "error:", f)
				}
				if len(results) > 0 {
					fmt.Fprintln(a.stdout, "check requirements with `maggot doctor`")
				}
			}
			if len(failed) > 0 {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "allow reinstalling the same version or downgrading")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func pastTense(action string) string {
	switch action {
	case "upgrade":
		return "upgraded"
	case "downgrade":
		return "downgraded"
	case "reinstall":
		return "reinstalled"
	}
	return "installed"
}

func (a *app) describeImport(s *pack.Staged, handle string) string {
	m := s.Manifest
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s into toolbox %q", s.Action, m.ID, m.Version, handle)
	if s.Existing != "" {
		fmt.Fprintf(&b, " (installed: %s)", s.Existing)
	}
	entry := m.Entry.Path
	if m.Entry.Bin != "" {
		entry = m.Entry.Bin
	}
	fmt.Fprintf(&b, "\n  %s\n  %d files, %d bytes\n  runs: %s", m.Description, s.Files, s.Bytes,
		strings.Join(append(append(append([]string{}, m.Entry.Interpreter...), entry), m.Entry.Args...), " "))
	if len(m.Requires) > 0 {
		bins := make([]string, len(m.Requires))
		for i, r := range m.Requires {
			bins[i] = r.Bin
		}
		fmt.Fprintf(&b, "\n  requires: %s", strings.Join(bins, ", "))
	}
	b.WriteString("\nThis tool is code that will run on this machine. Continue?")
	return b.String()
}

func (a *app) removeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <tool>",
		Short: "Delete a tool folder from its toolbox",
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
			if err := a.confirm(yes, fmt.Sprintf("delete %s (%s)?", t.Ref(), t.Dir)); err != nil {
				return err
			}
			if err := os.RemoveAll(t.Dir); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "removed %s\n", t.Ref())
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
