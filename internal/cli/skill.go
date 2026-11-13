package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/nxnom/maggot/internal/search"
	"github.com/nxnom/maggot/internal/skill"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/workspace"
	"github.com/spf13/cobra"
)

// skillSummary is the list/search row for a skill.
type skillSummary struct {
	Ref         string   `json:"ref"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Toolbox     string   `json:"toolbox"`
	Dir         string   `json:"dir"`
	Ready       bool     `json:"ready"`
	Missing     []string `json:"missing"`
}

func (a *app) summarizeSkill(e *env, sk *toolbox.Skill) skillSummary {
	store, _ := a.secretStore()
	c := workspace.CheckSkill(e.Set, sk, store, a.lookPath())
	return skillSummary{Ref: sk.Ref(), Name: sk.Meta.Name, Description: sk.Meta.Description, Version: sk.Version,
		Toolbox: sk.Toolbox.Handle, Dir: sk.Dir, Ready: c.Ready, Missing: c.Missing}
}

func (a *app) lookPath() func(string) (string, error) {
	if a.doctor != nil {
		return a.doctor.LookPath
	}
	return nil
}

// skillArg accepts a skill name ("name" or "toolbox/name") or a path to a
// skill folder, such as a draft.
func (a *app) skillArg(e *env, arg string) (*toolbox.Skill, error) {
	if strings.ContainsAny(arg, `/\`) && agentskill.Exists(arg) {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return nil, err
		}
		skills, _ := e.Set.Skills()
		for _, sk := range skills {
			if sk.Dir == abs {
				return sk, nil
			}
		}
		sk, err := agentskill.Load(abs)
		if err != nil {
			return nil, err
		}
		handle := "local"
		if drafts, _ := config.SkillDraftsDir(); filepath.Dir(abs) == drafts {
			handle = "draft"
		}
		return &toolbox.Skill{Skill: sk, Toolbox: &toolbox.Toolbox{Handle: handle, Path: filepath.Dir(abs)}}, nil
	}
	return e.Set.ResolveSkill(arg)
}

func (a *app) skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Agent skills: maggot's own skill, and skills kept in your toolboxes",
		Long: `Two kinds of skills:

  maggot's own skill teaches coding agents to use maggot. Install it with
  'maggot skill install' (no names).

  Toolbox skills are folders in the Agent Skills format (SKILL.md plus
  optional scripts/, references/ and assets/) kept in <toolbox>/skills/.
  They are step-by-step instructions for agents that may call maggot tools,
  their own scripts, other programs or APIs. Agents find them with
  'maggot search' and read them with 'maggot skill show <name>'.
  'maggot skill install <name>' also links one into Claude Code / Codex
  so the agent loads it on its own.

maggot fields go under "metadata" in the SKILL.md frontmatter:
  metadata:
    version: 1.0.0
    maggot-uses: voxcpm-tts depth-map     # maggot tools the steps call
    maggot-requires: ffmpeg node          # programs needed on PATH
    maggot-env: STABILITY_API_KEY         # secrets (maggot secret set --skill)`,
	}
	cmd.AddCommand(a.skillListCmd(), a.skillShowCmd(), a.skillNewCmd(), a.skillDraftCmd(),
		a.skillInstallCmd(), a.skillUninstallCmd(), a.skillExecCmd(), a.skillRunCmd(), a.skillPathCmd())
	return cmd
}

type agentFlags struct {
	agents      []string
	project     bool
	copy, force bool
}

func (f *agentFlags) add(c *cobra.Command, install bool) {
	c.Flags().StringSliceVar(&f.agents, "agent", []string{"claude", "codex"}, "agents: "+strings.Join(skill.Agents(), ", "))
	c.Flags().BoolVar(&f.project, "project", false, "use the current directory instead of the home folder")
	c.Flags().BoolVar(&f.force, "force", false, "replace or remove a folder of the same name that maggot did not make")
	if install {
		c.Flags().BoolVar(&f.copy, "copy", false, "copy the skill folder instead of linking it (a copy does not see later changes)")
	}
}

func (f *agentFlags) base() (string, error) {
	if f.project {
		return os.Getwd()
	}
	return os.UserHomeDir()
}

func (a *app) skillInstallCmd() *cobra.Command {
	var f agentFlags
	c := &cobra.Command{
		Use:   "install [skill...]",
		Short: "Install maggot's skill, or toolbox skills, into each agent's skills folder",
		Long: `Without names: write maggot's own SKILL.md for each agent:
  claude → ~/.claude/skills/maggot/SKILL.md
  codex  → ~/.codex/skills/maggot/SKILL.md
  agents → ~/.agents/skills/maggot/SKILL.md (shared folder some agents read)
Run it again after upgrading maggot to update the skill.

With names: link each toolbox skill folder into the same places
(~/.claude/skills/<name> -> <toolbox>/skills/<name>), so edits in the
toolbox show up right away. --copy copies the folder instead.

With --project the folders are created in the current directory instead.`,
		Example: `  maggot skill install
  maggot skill install video-short --agent claude`,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := f.base()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				for _, ag := range f.agents {
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
			}
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			for _, name := range args {
				sk, err := a.skillArg(e, name)
				if err != nil {
					return err
				}
				for _, ag := range f.agents {
					p, state, err := skill.Link(b, strings.TrimSpace(ag), sk.Meta.Name, sk.Dir, f.copy, f.force, pack.CopyTool)
					if err != nil {
						return err
					}
					fmt.Fprintf(a.stdout, "%s: %s (%s)\n", ag, p, state)
				}
			}
			return nil
		},
	}
	f.add(c, true)
	return c
}

func (a *app) skillUninstallCmd() *cobra.Command {
	var f agentFlags
	c := &cobra.Command{
		Use:   "uninstall [skill...]",
		Short: "Remove maggot's skill, or toolbox skills, from each agent's skills folder",
		Long: `Without names: remove maggot's own skill folder for each agent.
With names: remove the links or copies 'maggot skill install <name>' made.
The skill stays in its toolbox.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := f.base()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				for _, ag := range f.agents {
					p, err := skill.Uninstall(b, strings.TrimSpace(ag))
					if err != nil {
						return err
					}
					fmt.Fprintf(a.stdout, "%s: removed %s\n", ag, p)
				}
				return nil
			}
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			for _, name := range args {
				src, skillName := "", name
				if sk, err := a.skillArg(e, name); err == nil {
					src, skillName = sk.Dir, sk.Meta.Name
				} else if !f.force {
					return err
				}
				for _, ag := range f.agents {
					p, removed, err := skill.Unlink(b, strings.TrimSpace(ag), skillName, src, f.force)
					if err != nil {
						return err
					}
					if removed {
						fmt.Fprintf(a.stdout, "%s: removed %s\n", ag, p)
					} else {
						fmt.Fprintf(a.stdout, "%s: not installed\n", ag)
					}
				}
			}
			return nil
		},
	}
	f.add(c, false)
	return c
}

func (a *app) skillListCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List skills in all toolboxes, with whether each is ready",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			skills, problems := e.Set.Skills()
			rows := []skillSummary{}
			for _, sk := range skills {
				rows = append(rows, a.summarizeSkill(e, sk))
			}
			if asJSON {
				if problems == nil {
					problems = []toolbox.Problem{}
				}
				return writeJSON(a.stdout, map[string]any{"skills": rows, "problems": problems})
			}
			a.warnProblems(problems)
			if len(rows) == 0 {
				fmt.Fprintln(a.stdout, "no skills; create one with `maggot skill new <name>`")
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "SKILL\tVERSION\tREADY\tDESCRIPTION")
			for _, r := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Ref, dash(r.Version), readyLabel(r.Ready, r.Missing), oneLine(r.Description, 90))
			}
			return w.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func readyLabel(ready bool, missing []string) string {
	if ready {
		return "yes"
	}
	return "needs " + strings.Join(missing, ", ")
}

// oneLine shortens text to one line of at most n characters.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

type agentState struct {
	Agent string `json:"agent"`
	State string `json:"state"` // none, linked, copied, other
	Path  string `json:"path"`
}

func skillAgents(sk *toolbox.Skill) []agentState {
	home, err := os.UserHomeDir()
	if err != nil {
		return []agentState{}
	}
	out := []agentState{}
	for _, ag := range skill.AgentsInOrder() {
		state, p, err := skill.State(home, ag, sk.Meta.Name, sk.Dir)
		if err == nil {
			out = append(out, agentState{Agent: ag, State: state, Path: p})
		}
	}
	return out
}

// skillFiles lists the files of a skill folder except SKILL.md, sorted.
func skillFiles(dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "__pycache__" || d.Name() == ".venv" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel != agentskill.FileName && !strings.HasPrefix(d.Name(), ".") && len(out) < 500 {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

func (a *app) skillShowCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show [skill]",
		Short: "Show a toolbox skill: its steps, files and whether it is ready (no name: print maggot's own SKILL.md)",
		Long: `Show what an agent needs to follow a toolbox skill: the folder (scripts
and assets are relative to it), the readiness check (maggot tools it uses,
programs, secrets), its files and the SKILL.md instructions.

Without a name it prints maggot's own SKILL.md.`,
		Example: `  maggot skill show video-short
  maggot skill show video-short --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_, err := fmt.Fprint(a.stdout, skill.Text)
				return err
			}
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			sk, err := a.skillArg(e, args[0])
			if err != nil {
				return err
			}
			store, _ := a.secretStore()
			check := workspace.CheckSkill(e.Set, sk, store, a.lookPath())
			files := skillFiles(sk.Dir)
			agents := skillAgents(sk)
			if asJSON {
				return writeJSON(a.stdout, map[string]any{
					"ref": sk.Ref(), "name": sk.Meta.Name, "description": sk.Meta.Description, "version": sk.Version,
					"toolbox": sk.Toolbox.Handle, "dir": sk.Dir, "skill_md": filepath.Join(sk.Dir, agentskill.FileName),
					"meta": sk.Meta, "check": check, "files": files, "agents": agents, "body": sk.Body,
				})
			}
			a.printSkill(sk, check, files, agents)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func (a *app) printSkill(sk *toolbox.Skill, c workspace.SkillCheck, files []string, agents []agentState) {
	w := a.stdout
	version := ""
	if sk.Version != "" {
		version = " " + sk.Version
	}
	fmt.Fprintf(w, "%s%s  (%s)\n%s\n\n", sk.Meta.Name, version, sk.Ref(), sk.Meta.Description)
	fmt.Fprintf(w, "folder: %s\n", sk.Dir)
	if c.Ready {
		fmt.Fprintln(w, "ready:  yes")
	} else {
		fmt.Fprintf(w, "ready:  no, missing %s\n", strings.Join(c.Missing, ", "))
	}
	for _, t := range c.Tools {
		switch {
		case !t.Found:
			fmt.Fprintf(w, "  ✗ tool %s: not installed (search with `maggot search`, or build it)\n", t.Name)
		case len(t.Missing) > 0:
			fmt.Fprintf(w, "  ✗ tool %s: needs %s (`maggot doctor %s`)\n", t.Ref, strings.Join(t.Missing, ", "), t.Ref)
		default:
			fmt.Fprintf(w, "  ✓ tool %s (`maggot info %s`)\n", t.Ref, t.Ref)
		}
	}
	for _, b := range c.Requires {
		if b.Found {
			fmt.Fprintf(w, "  ✓ %s %s\n", b.Bin, b.Path)
		} else {
			fmt.Fprintf(w, "  ✗ %s: not on PATH\n", b.Bin)
		}
	}
	for _, e := range c.Env {
		if e.Set {
			fmt.Fprintf(w, "  ✓ env %s (%s)\n", e.Name, e.Source)
		} else {
			fmt.Fprintf(w, "  ✗ env %s: not set (the user runs `maggot secret set --skill %s %s`)\n", e.Name, sk.Meta.Name, e.Name)
		}
	}
	if len(c.Env) > 0 {
		fmt.Fprintf(w, "run commands that need the secrets with: maggot skill exec %s -- <command>\n", sk.Meta.Name)
	}
	var installed []string
	for _, ag := range agents {
		if ag.State == skill.StateLinked || ag.State == skill.StateCopied {
			installed = append(installed, ag.Agent)
		}
	}
	if len(installed) > 0 {
		fmt.Fprintf(w, "agents: installed for %s\n", strings.Join(installed, ", "))
	}
	if hasScripts(files) {
		fmt.Fprintf(w, "run scripts with: maggot skill run %s <script> [args]   (read files: maggot skill path %s <file>)\n", sk.Meta.Name, sk.Meta.Name)
	}
	if len(files) > 0 {
		fmt.Fprintln(w, "files (relative to the folder):")
		for _, f := range files {
			fmt.Fprintf(w, "  %s\n", f)
		}
	}
	fmt.Fprintf(w, "\n----- %s -----\n%s", agentskill.FileName, sk.Body)
	if !strings.HasSuffix(sk.Body, "\n") {
		fmt.Fprintln(w)
	}
}

func (a *app) skillNewCmd() *cobra.Command {
	var dir, desc string
	c := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new skill folder with a SKILL.md template",
		Long: `Create <dir>/<name>/SKILL.md in the Agent Skills format. By default it goes
to the maggot drafts folder; edit it, let the user read it in 'maggot ui',
then 'maggot import' the folder into the toolbox.

Put scripts the steps run in scripts/, long docs in references/ and
templates, images or fonts in assets/. Call scripts in the steps with
'maggot skill run <name> <script> [args]' (no path needed). Use a maggot tool instead of a script
when the job is useful on its own (it gets a form, tests and search).`,
		Example: `  maggot skill new video-short --description "Make a 60 s horror short from stills. Use when ..."`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := agentskill.ValidName(name); err != nil {
				return err
			}
			if dir == "" {
				var err error
				if dir, err = config.SkillDraftsDir(); err != nil {
					return err
				}
			}
			target := filepath.Join(dir, name)
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("%s already exists", target)
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(target, agentskill.FileName), []byte(agentskill.Template(name, desc)), 0o644); err != nil {
				return err
			}
			a.warnSimilarSkills(name, desc)
			abs, _ := filepath.Abs(target)
			fmt.Fprintf(a.stdout, "created %s\nnext: write the steps in SKILL.md, `maggot skill show %s` to check it, then `maggot import %s`\n", abs, abs, abs)
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "parent folder for the new skill (default: the maggot drafts folder)")
	c.Flags().StringVar(&desc, "description", "", "what the skill does and when to use it")
	return c
}

// warnSimilarSkills lists skills that look like a new one, so agents improve
// a skill instead of adding a near-duplicate.
func (a *app) warnSimilarSkills(name, desc string) {
	e, err := a.loadEnv()
	if err != nil {
		return
	}
	skills, _ := e.Set.Skills()
	var similar []string
	for _, h := range search.RankSkills(skills, strings.ReplaceAll(name, "-", " ")+" "+desc) {
		if len(h.Matched) >= 2 || h.Skill.Meta.Name == name {
			similar = append(similar, fmt.Sprintf("  %s — %s", h.Skill.Ref(), oneLine(h.Skill.Meta.Description, 100)))
		}
		if len(similar) == 5 {
			break
		}
	}
	if len(similar) > 0 {
		fmt.Fprintf(a.stderr, "note: similar skills already exist; if one almost fits, improve it (`maggot skill draft <name>`) instead:\n%s\n",
			strings.Join(similar, "\n"))
	}
}

func (a *app) skillDraftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "draft <skill>",
		Short: "Copy a toolbox skill into the drafts folder to change it",
		Long: `Copy a toolbox skill into the drafts folder. Edit the copy, bump
metadata.version, let the user see the changes in 'maggot ui', then
'maggot import' it to replace the toolbox skill.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			sk, err := e.Set.ResolveSkill(args[0])
			if err != nil {
				return err
			}
			drafts, err := config.SkillDraftsDir()
			if err != nil {
				return err
			}
			dst := filepath.Join(drafts, sk.Meta.Name)
			if err := os.MkdirAll(drafts, 0o755); err != nil {
				return err
			}
			if err := pack.CopyTool(sk.Dir, dst); err != nil {
				return fmt.Errorf("%w (delete or finish the existing draft first)", err)
			}
			fmt.Fprintf(a.stdout, "%s\nedit it, bump metadata.version in SKILL.md, then `maggot import %s`\n", dst, dst)
			return nil
		},
	}
}

func (a *app) skillExecCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "exec <skill> -- <command> [args...]",
		Short: "Run a command with a skill's secrets in the environment",
		Long: `Run one command (usually one of the skill's scripts) with the secrets the
skill declares in metadata.maggot-env added to the environment, plus
MAGGOT_SKILL_DIR set to the skill folder. The command runs in the current
directory; stdin, stdout, stderr and the exit code pass through. Secret
values are never printed.`,
		Example: `  maggot skill exec video-short -- ffmpeg -i in.mp4 out.mp3   (to run a script, use 'maggot skill run')`,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 1 {
				return errors.New("put -- between the skill and the command: maggot skill exec <skill> -- <command> [args...]")
			}
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			sk, err := a.skillArg(e, args[0])
			if err != nil {
				return err
			}
			return a.runInSkill(cmd, sk, args[1], args[2:])
		},
	}
	return c
}

// runInSkill runs one command with the skill's secrets and MAGGOT_SKILL_DIR in
// the environment. The command runs in the current directory.
func (a *app) runInSkill(cmd *cobra.Command, sk *toolbox.Skill, name string, args []string) error {
	store, err := a.secretStore()
	if err != nil {
		return err
	}
	extra, err := workspace.SkillSecretEnv(store, sk)
	if err != nil {
		return err
	}
	var missing []string
	for _, s := range workspace.SkillEnv(store, sk) {
		if !s.Set {
			missing = append(missing, s.Name)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(a.stderr, "warning: %s not set; the user can run `maggot secret set --skill %s <NAME>`\n", strings.Join(missing, ", "), sk.Meta.Name)
	}
	c := exec.CommandContext(cmd.Context(), name, args...)
	c.Env = append(append(os.Environ(), extra...), "MAGGOT_SKILL_DIR="+sk.Dir)
	c.Stdin, c.Stdout, c.Stderr = a.stdin, a.stdout, a.stderr
	return a.wait(c)
}

func (a *app) skillRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run <skill> <script> [args...]",
		Short: "Run one of a skill's scripts (no path, no interpreter needed)",
		Long: `Run a script from the skill folder. <script> is a file in the skill's
scripts/ folder (or a path inside the skill folder, like scripts/x.py). maggot
picks the interpreter from the file type:

  .py   uv run --script when the file has a PEP 723 header ("# /// script"),
        otherwise python3
  .sh   sh            .js, .mjs  node            .ts  deno run -A
  other files must be executable and are run directly

The script runs in the current directory, so relative paths in the arguments
(scene.json, out/) mean the caller's folder, not the skill folder. The skill's
secrets (metadata.maggot-env) and MAGGOT_SKILL_DIR are added to the
environment. Everything after the script name goes to the script as it is.`,
		Example: `  maggot skill run cartoon-from-storyboard studio.py render scene.json --frames 1,3.5
  maggot skill run video-short sfx.py rain.wav`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			sk, err := a.skillArg(e, args[0])
			if err != nil {
				return err
			}
			argv, err := skillScriptCommand(sk.Dir, args[1], a.lookPath())
			if err != nil {
				return err
			}
			return a.runInSkill(cmd, sk, argv[0], append(argv[1:], args[2:]...))
		},
	}
	c.Flags().SetInterspersed(false) // flags after <script> belong to the script
	return c
}

func (a *app) skillPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <skill> [file]",
		Short: "Print the skill folder, or the full path of a file inside it",
		Long: `Print the absolute folder of a skill. With a file, print its absolute path
(it must exist inside the skill folder). Use it to read references/ and
assets/ files without knowing where the toolbox is. To run a script, use
'maggot skill run' instead.`,
		Example: `  cat "$(maggot skill path cartoon-from-storyboard references/prompts.md)"`,
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			sk, err := a.skillArg(e, args[0])
			if err != nil {
				return err
			}
			out := sk.Dir
			if len(args) == 2 {
				if out, err = skillFile(sk.Dir, args[1]); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintln(a.stdout, out)
			return err
		},
	}
}
