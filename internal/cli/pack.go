package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/nxnom/maggot/internal/skill"
	"github.com/nxnom/maggot/internal/testlog"
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
	if pack.KindOf(dir) == pack.KindSkill {
		sk, err := agentskill.Load(dir)
		if err != nil {
			return err
		}
		dest := out
		if dest == "" {
			dest = pack.SkillFileName(sk)
		} else if info, err := os.Stat(out); err == nil && info.IsDir() {
			dest = filepath.Join(out, pack.SkillFileName(sk))
		}
		if _, err := pack.PackSkill(dir, dest); err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, dest)
		return nil
	}
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

// toolOrSkillDir resolves a tool reference, or a skill when no tool has
// that name. It returns the folder and a label.
func (a *app) toolOrSkillDir(e *env, ref string) (string, string, error) {
	t, err := e.Set.Resolve(ref)
	if err == nil {
		return t.Dir, t.Ref(), nil
	}
	if sk, skErr := e.Set.ResolveSkill(ref); skErr == nil {
		return sk.Dir, "skill " + sk.Ref(), nil
	}
	return "", "", err
}

func (a *app) packCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "pack <tool-dir|skill-dir>",
		Short: "Pack a tool or skill folder into a .maggot archive",
		Long:  "Pack a tool or skill folder into a .maggot archive (a zip). .git, .DS_Store, __pycache__ and paths listed in .maggotignore are left out.",
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
		Use:   "export <tool|skill>",
		Short: "Export an installed tool or skill to a .maggot archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			dir, _, err := a.toolOrSkillDir(e, args[0])
			if err != nil {
				return err
			}
			return a.packDir(dir, out)
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output file or folder (default: <id>-<version>.maggot)")
	return cmd
}

func (a *app) importCmd() *cobra.Command {
	var force, yes, asJSON, keepDraft bool
	cmd := &cobra.Command{
		Use:   "import <file.maggot|file.zip|folder|draft-name>...",
		Short: "Install tools or skills into a toolbox from archives or folders",
		Long: `Install tools (tool.json) or skills (SKILL.md) into the default toolbox (or
--toolbox): tools go to tools/<id>, skills to skills/<name>. Each source is
validated in a temp folder first. Imported tools and skill scripts are code
that will run on this machine, so maggot shows what will be installed and
asks first; pass --yes only after the user approved.

Installing an older or the same version over an existing one needs --force.`,
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
				src = draftPath(src)
				s, err := pack.Stage(src, tb.ToolsPath())
				if err != nil {
					failed = append(failed, fmt.Sprintf("%s: %v", src, err))
					continue
				}
				if s.NeedsForce() && !force {
					s.Discard()
					failed = append(failed, fmt.Sprintf("%s: %s %s is installed and this is %s; use --force to %s",
						src, s.ID, s.Existing, dash(s.Version), s.Action))
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
				if s.Kind == pack.KindTool {
					// A test made on the draft counts for the installed tool if nothing changed.
					if info, err := os.Stat(src); err == nil && info.IsDir() {
						if testlog.Adopt(testlog.DraftKey(s.ID), tb.Handle+"/"+s.ID, s.Version, testlog.DirHash(src)) && !asJSON {
							fmt.Fprintln(a.stdout, "kept the test result of the draft")
						}
					}
				}
				if !asJSON {
					kind := ""
					if s.Kind == pack.KindSkill {
						kind = "skill "
					}
					fmt.Fprintf(a.stdout, "%s %s%s/%s %s\n", pastTense(s.Action), kind, tb.Handle, s.ID, s.Version)
				}
				// A draft that is now installed would be a second copy of the tool.
				if !keepDraft && isDraft(src) {
					if err := os.RemoveAll(src); err == nil && !asJSON {
						fmt.Fprintf(a.stdout, "deleted the draft %s\n", src)
					}
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
				for _, r := range results {
					if r.Kind == pack.KindTool {
						fmt.Fprintln(a.stdout, "check requirements with `maggot doctor`")
						break
					}
				}
				for _, r := range results {
					if r.Kind == pack.KindSkill {
						fmt.Fprintf(a.stdout, "check it with `maggot skill show %s`; link it into agents with `maggot skill install %s`\n", r.ID, r.ID)
					}
				}
			}
			if len(failed) > 0 {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "allow reinstalling the same version or downgrading")
	cmd.Flags().BoolVar(&keepDraft, "keep-draft", false, "keep the folder when importing from the drafts folder")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// isDraft reports whether src is a tool or skill folder directly inside a
// drafts folder.
func isDraft(src string) bool {
	drafts, err := config.DraftsDir()
	if err != nil {
		return false
	}
	skillDrafts, _ := config.SkillDraftsDir()
	abs, err := filepath.Abs(src)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	parent := filepath.Dir(abs)
	return err == nil && info.IsDir() && (parent == drafts || parent == skillDrafts) && abs != skillDrafts
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
	if s.Kind == pack.KindSkill {
		sk := s.Skill
		var b strings.Builder
		fmt.Fprintf(&b, "%s skill %s %s into toolbox %q", s.Action, sk.Meta.Name, dash(sk.Version), handle)
		if s.Existing != "" {
			fmt.Fprintf(&b, " (installed: %s)", s.Existing)
		}
		fmt.Fprintf(&b, "\n  %s\n  %d files, %d bytes", oneLine(sk.Meta.Description, 200), s.Files, s.Bytes)
		if len(sk.Uses) > 0 {
			fmt.Fprintf(&b, "\n  uses tools: %s", strings.Join(sk.Uses, ", "))
		}
		if len(sk.Requires) > 0 {
			fmt.Fprintf(&b, "\n  requires: %s", strings.Join(sk.Requires, ", "))
		}
		b.WriteString("\nAgents follow this skill and may run its scripts on this machine. Continue?")
		return b.String()
	}
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
		Use:   "remove <tool|skill>",
		Short: "Delete a tool or skill folder from its toolbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			dir, label, err := a.toolOrSkillDir(e, args[0])
			if err != nil {
				return err
			}
			if err := a.confirm(yes, fmt.Sprintf("delete %s (%s)?", label, dir)); err != nil {
				return err
			}
			// A skill linked into agents would leave a broken link behind.
			if sk, err := e.Set.ResolveSkill(args[0]); err == nil && sk.Dir == dir {
				if home, err := os.UserHomeDir(); err == nil {
					for _, ag := range skill.Agents() {
						if p, removed, err := skill.Unlink(home, ag, sk.Meta.Name, sk.Dir, false); err == nil && removed {
							fmt.Fprintf(a.stdout, "%s: removed %s\n", ag, p)
						}
					}
				}
			}
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "removed %s\n", label)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
