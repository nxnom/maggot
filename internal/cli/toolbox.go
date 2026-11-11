package cli

import (
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/spf13/cobra"
)

func (a *app) toolboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "toolbox",
		Short: "Create and register toolbox folders",
	}
	cmd.AddCommand(a.toolboxInitCmd(), a.toolboxAddCmd(), a.toolboxListCmd(), a.toolboxRemoveCmd(), a.toolboxDefaultCmd())
	return cmd
}

func (a *app) toolboxInitCmd() *cobra.Command {
	var name string
	var noRegister bool
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Create a new toolbox (toolbox.json + tools/) and register it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if name == "" {
				name = handleFor(abs, "")
			}
			tb, err := toolbox.Init(abs, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "created toolbox %q at %s\n", tb.Meta.Name, tb.Path)
			if noRegister {
				return nil
			}
			return a.register(tb.Path, name, false)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "toolbox name (default: folder name)")
	cmd.Flags().BoolVar(&noRegister, "no-register", false, "only create the files, do not register")
	return cmd
}

func (a *app) toolboxAddCmd() *cobra.Command {
	var name string
	var makeDefault bool
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Register an existing toolbox folder (for example a git checkout)",
		Long:  "Register an existing toolbox folder. Registering a toolbox means you trust the tools in it to run on this machine.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tb, err := toolbox.Open(args[0], "")
			if err != nil {
				return err
			}
			if name == "" {
				name = handleFor(tb.Path, tb.Meta.Name)
			}
			return a.register(tb.Path, name, makeDefault)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "short name used in tool references (default: from toolbox.json)")
	cmd.Flags().BoolVar(&makeDefault, "default", false, "make it the default toolbox for imports and new tools")
	return cmd
}

func (a *app) register(path, name string, makeDefault bool) error {
	e, err := a.loadEnv()
	if err != nil {
		return err
	}
	if err := e.Config.Add(name, path); err != nil {
		return err
	}
	if makeDefault {
		e.Config.Default = name
	}
	if err := e.Config.Save(); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "registered toolbox %q (%s)\n", name, path)
	return nil
}

func (a *app) toolboxListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered toolboxes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			type row struct {
				Name    string `json:"name"`
				Path    string `json:"path"`
				Default bool   `json:"default"`
				Tools   int    `json:"tools"`
				OK      bool   `json:"ok"`
				Error   string `json:"error,omitempty"`
			}
			var rows []row
			for _, ref := range e.Config.Toolboxes {
				r := row{Name: ref.Name, Path: ref.Path, Default: ref.Name == e.Config.Default}
				if tb, err := toolbox.Open(ref.Path, ref.Name); err != nil {
					r.Error = err.Error()
				} else {
					tools, _ := tb.Tools()
					r.OK, r.Tools = true, len(tools)
				}
				rows = append(rows, r)
			}
			if asJSON {
				if rows == nil {
					rows = []row{}
				}
				return writeJSON(a.stdout, map[string]any{"toolboxes": rows, "config": e.Config.Path()})
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.stdout, "no toolboxes registered; create one with `maggot toolbox init <path>`")
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTOOLS\tPATH")
			for _, r := range rows {
				name := r.Name
				if r.Default {
					name += " (default)"
				}
				tools := fmt.Sprint(r.Tools)
				if !r.OK {
					tools = "missing"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", name, tools, r.Path)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func (a *app) toolboxRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Unregister a toolbox (files are not deleted)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			if err := e.Config.Remove(args[0]); err != nil {
				return err
			}
			if err := e.Config.Save(); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "unregistered toolbox %q\n", args[0])
			return nil
		},
	}
}

func (a *app) toolboxDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "default <name>",
		Short: "Set the default toolbox for imports",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			if err := e.Config.SetDefault(args[0]); err != nil {
				return err
			}
			return e.Config.Save()
		},
	}
}

// targetToolbox is where new or imported tools go.
func (a *app) targetToolbox(e *env) (*toolbox.Toolbox, error) { return e.Target() }
