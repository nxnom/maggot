package cli

import (
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/nxnom/maggot/internal/warm"
	"github.com/spf13/cobra"
)

func (a *app) warmCmd() *cobra.Command {
	var idle, wait string
	cmd := &cobra.Command{
		Use:   "warm <tool>",
		Short: "Keep a slow-loading tool in memory so the next runs start fast",
		Long: `Start a tool's serve mode in the background. It loads once (for example a
model) and handles the next runs of the tool until it has had no job for the
--for time (default 10m); each job resets that timer. Runs still work when
the tool is not warm, they just load again.

The tool must declare "warm" in tool.json (see 'maggot guide'). A warm tool
keeps its memory (and GPU memory) in use, so warm it only when several runs
are coming, and stop it when done.`,
		Example: `  maggot warm voice-clone --for 30m
  maggot warm list
  maggot warm stop voice-clone`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idleD, err := parseAge(idle)
			if err != nil {
				return err
			}
			waitD, err := parseAge(wait)
			if err != nil {
				return err
			}
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			t, err := e.Set.Resolve(args[0])
			if err != nil {
				return err
			}
			if err := a.preflight(t); err != nil {
				return err
			}
			env, err := a.secretEnv(t)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stderr, "loading %s…\n", t.Ref())
			st, err := warm.Start(t, idleD, waitD, env)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "%s is warm; it stops after %s without a job (`maggot warm stop %s` to stop now)\n",
				t.Ref(), st.Idle, t.Manifest.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&idle, "for", "10m", "stop after this long without a job, e.g. 15m, 2h")
	cmd.Flags().StringVar(&wait, "wait", "5m", "how long to wait for loading to finish")

	list := &cobra.Command{
		Use:   "list",
		Short: "Show warm tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			all := warm.List()
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				if all == nil {
					all = []warm.Status{}
				}
				return writeJSON(a.stdout, map[string]any{"warm": all})
			}
			if len(all) == 0 {
				fmt.Fprintln(a.stdout, "no warm tools")
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TOOL\tSTATE\tJOBS\tSTOPS IN\tSINCE")
			for _, s := range all {
				state, left := "loading", "-"
				if s.Running {
					state = "ready"
					left = (time.Duration(s.IdleLeft) * time.Second).String()
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", s.Ref, state, s.Jobs, left, s.Started.Format(time.Kitchen))
			}
			return w.Flush()
		},
	}
	list.Flags().Bool("json", false, "print JSON")

	var stopAll bool
	stop := &cobra.Command{
		Use:   "stop <tool>... | --all",
		Short: "Stop warm tools and free their memory",
		RunE: func(cmd *cobra.Command, args []string) error {
			ids := args
			if stopAll {
				ids = nil
				for _, s := range warm.List() {
					ids = append(ids, s.ID)
				}
			} else if len(ids) == 0 {
				return errors.New("name a tool or use --all")
			}
			for _, ref := range ids {
				id := ref
				if e, err := a.loadEnv(); err == nil {
					if t, err := e.Set.Resolve(ref); err == nil {
						id = t.Manifest.ID
					}
				}
				if err := warm.Stop(id); err != nil {
					return err
				}
				fmt.Fprintf(a.stdout, "stopped %s\n", id)
			}
			return nil
		},
	}
	stop.Flags().BoolVar(&stopAll, "all", false, "stop every warm tool")
	cmd.AddCommand(list, stop)
	return cmd
}
