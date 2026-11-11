package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/workspace"
)

// env is the loaded workspace (config + active toolboxes).
type env = workspace.Workspace

// loadEnv loads the workspace for --toolbox / $MAGGOT_TOOLBOX and the cwd.
func (a *app) loadEnv() (*env, error) {
	sel := a.toolboxFlag
	if sel == "" {
		sel = os.Getenv("MAGGOT_TOOLBOX")
	}
	cwd, _ := os.Getwd()
	return workspace.Load(sel, cwd)
}

func handleFor(dir, metaName string) string { return workspace.HandleFor(dir, metaName) }

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func (a *app) warnProblems(problems []toolbox.Problem) {
	for _, p := range problems {
		fmt.Fprintf(a.stderr, "warning: %s: %s\n", p.Path, firstLine(p.Error))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
