package cli

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/nxnom/maggot/internal/presets"
	"github.com/spf13/cobra"
)

func (a *app) presetsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "presets <tool>",
		Short: "List saved input presets of a tool",
		Long: `Presets are named sets of input values ("narrator voice", "1080p MP4").
Personal presets are kept on this machine; toolbox presets live in the tool
folder (tools/<id>/presets/) and travel with the toolbox. A personal preset
wins over a toolbox preset with the same name; pick one with personal/<name>
or toolbox/<name>. Use one with: maggot run <tool> --preset <name>.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			t, err := e.Set.Resolve(args[0])
			if err != nil {
				return err
			}
			list := presets.List(t.Manifest.ID, t.Dir)
			if asJSON {
				if list == nil {
					list = []presets.Preset{}
				}
				return writeJSON(a.stdout, map[string]any{"ref": t.Ref(), "presets": list})
			}
			if len(list) == 0 {
				fmt.Fprintf(a.stdout, "no presets; save one with: maggot presets save %s <name> --inputs '{...}'\n", t.Ref())
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "PRESET\tLABEL\tVALUES")
			for _, p := range list {
				fmt.Fprintf(w, "%s/%s\t%s\t%v\n", p.Scope, p.Name, p.Label, p.Values)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")

	var inputs, label, desc string
	var toToolbox bool
	save := &cobra.Command{
		Use:   "save <tool> <name> --inputs '{...}'",
		Short: "Save input values as a preset (personal unless --toolbox)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputs == "" {
				return errors.New("give the values with --inputs '{...}'")
			}
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			t, err := e.Set.Resolve(args[0])
			if err != nil {
				return err
			}
			values, err := readInputs(inputs)
			if err != nil {
				return err
			}
			known := map[string]bool{}
			for _, in := range t.Manifest.Inputs {
				known[in.Name] = true
			}
			for k := range values {
				if !known[k] {
					return fmt.Errorf("%s has no input %q (see `maggot info %s`)", t.Ref(), k, t.Ref())
				}
			}
			scope := presets.Personal
			if toToolbox {
				scope = presets.Toolbox
			}
			name := args[1]
			if !presets.ValidName(name) {
				if label == "" {
					label = name
				}
				name = presets.Slug(name)
			}
			warnings, err := presets.Save(t.Manifest.ID, t.Dir, presets.Preset{Name: name, Label: label, Description: desc, Values: values, Scope: scope})
			if err != nil {
				return err
			}
			for _, w := range warnings {
				fmt.Fprintln(a.stderr, "warning:", w)
			}
			fmt.Fprintf(a.stdout, "saved %s/%s; use it with: maggot run %s --preset %s\n", scope, name, t.Ref(), name)
			return nil
		},
	}
	save.Flags().StringVar(&inputs, "inputs", "", "input values as JSON, or @file.json")
	save.Flags().StringVar(&label, "label", "", "display name")
	save.Flags().StringVar(&desc, "description", "", "what the preset is for")
	save.Flags().BoolVar(&toToolbox, "toolbox", false, "save into the tool folder (shared with the toolbox) instead of only on this machine")

	rm := &cobra.Command{
		Use:   "rm <tool> <name>",
		Short: "Delete a preset (name, personal/name or toolbox/name)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			t, err := e.Set.Resolve(args[0])
			if err != nil {
				return err
			}
			return presets.Delete(t.Manifest.ID, t.Dir, args[1])
		},
	}
	cmd.AddCommand(save, rm)
	return cmd
}
