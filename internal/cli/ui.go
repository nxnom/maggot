package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nxnom/maggot/internal/ui"
	"github.com/spf13/cobra"
)

func (a *app) uiCmd() *cobra.Command {
	var port int
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the local web UI (forms for every tool, live runs, output files)",
		Long: `Start the web UI on 127.0.0.1 and open it in the browser. Only this
computer can reach it, and only through the link printed here (it carries an
access token). Runs are saved under the maggot data folder
($MAGGOT_DATA_DIR, $XDG_DATA_HOME/maggot or ~/.local/share/maggot).
Press Ctrl-C to stop; active runs are cancelled.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sel := a.toolboxFlag
			if sel == "" {
				sel = os.Getenv("MAGGOT_TOOLBOX")
			}
			srv, err := ui.New(Version, sel)
			if err != nil {
				return err
			}
			ln, url, err := srv.Listen(port)
			if err != nil {
				return err
			}
			httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			fmt.Fprintf(a.stdout, "maggot ui: %s\nPress Ctrl-C to stop.\n", url)
			if !noOpen {
				if err := ui.OpenBrowser(url); err != nil {
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
		},
	}
	cmd.Flags().IntVar(&port, "port", 4545, "port on 127.0.0.1 (another free port is used if it is busy)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the browser")
	return cmd
}
