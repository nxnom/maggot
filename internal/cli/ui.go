package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) uiCmd() *cobra.Command {
	var port int
	var noOpen bool
	var try []string
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the local web UI (forms for every tool, live runs, output files)",
		Long: `Start the web UI on 127.0.0.1 and open it in the browser. Only this
computer can reach it, and only through the link printed here (it carries an
access token). Runs are saved under the maggot data folder
($MAGGOT_DATA_DIR, $XDG_DATA_HOME/maggot or ~/.local/share/maggot).
Press Ctrl-C to stop. Runs started in the UI keep going after it stops; open
the UI again to see them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.serveUI(port, noOpen, try)
		},
	}
	cmd.Flags().IntVar(&port, "port", ui.DefaultPort, "port on 127.0.0.1 (another free port is used if it is busy)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the browser")
	cmd.Flags().StringArrayVar(&try, "try", nil, "tool folder to try (repeatable); not saved")
	return cmd
}

// uiRunCmd is the background process that owns one run started in the UI.
func (a *app) uiRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "ui-run <spec>",
		Short:  "Run one tool for the web UI (internal)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return ui.RunWorker(args[0])
		},
	}
}

func (a *app) tryCmd() *cobra.Command {
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "try <tool-folder>",
		Short: "Try a tool folder in the web UI before importing it",
		Long: `Open a tool folder (for example a draft an agent just made) in the web UI,
where you can run it and import it when you are happy with it. The folder is
not added to your toolboxes or saved in the config. A running 'maggot ui' is
reused; otherwise one is started.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			if _, err := manifest.Load(dir); err != nil {
				return err
			}
			if base, token, ok := ui.Running(); ok {
				ref, err := ui.TryRemote(base, token, dir)
				if err != nil {
					return err
				}
				link := ui.ToolURL(base, token, ref)
				fmt.Fprintf(a.stdout, "opened in the running maggot ui: %s\n", link)
				if !noOpen {
					ui.OpenBrowser(link)
				}
				return nil
			}
			return a.serveUI(ui.DefaultPort, noOpen, []string{dir})
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the browser")
	return cmd
}

// serveUI runs the web UI until Ctrl-C. tryDirs are tool folders to try; the
// browser opens on the first one.
func (a *app) serveUI(port int, noOpen bool, tryDirs []string) error {
	sel := a.toolboxFlag
	if sel == "" {
		sel = os.Getenv("MAGGOT_TOOLBOX")
	}
	srv, err := ui.New(Version, sel)
	if err != nil {
		return err
	}
	// Runs go to a background maggot process, so they outlive the UI.
	if exe, err := os.Executable(); err == nil {
		srv.Worker = []string{exe, "ui-run"}
	}
	focus := ""
	for _, d := range tryDirs {
		ref, err := srv.AddTry(d)
		if err != nil {
			return err
		}
		if focus == "" {
			focus = ref
		}
	}
	ln, _, err := srv.Listen(port)
	if err != nil {
		return err
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	base := fmt.Sprintf("http://127.0.0.1:%d", actual)
	link := ui.ToolURL(base, srv.Token, focus)
	ui.MarkRunning(actual)
	defer ui.MarkStopped()

	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(a.stdout, "maggot ui: %s\nPress Ctrl-C to stop.\n", link)
	if !noOpen {
		if err := ui.OpenBrowser(link); err != nil {
			fmt.Fprintln(a.stderr, "could not open a browser; open the link above")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		fmt.Fprintln(a.stderr, "\nstopping…")
	}
	srv.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdown)
}
