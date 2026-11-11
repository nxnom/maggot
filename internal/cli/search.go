package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/nxnom/maggot/internal/search"
	"github.com/spf13/cobra"
)

func (a *app) searchCmd() *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "search <words...>",
		Short: "Find tools by what they do (matches id, name, tags, description, inputs)",
		Example: `  maggot search remove image background
  maggot search convert video --json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.loadEnv()
			if err != nil {
				return err
			}
			tools, _ := e.Set.Tools()
			hits := search.Rank(tools, strings.Join(args, " "))
			if limit > 0 && len(hits) > limit {
				hits = hits[:limit]
			}
			if asJSON {
				type row struct {
					toolSummary
					Score   int      `json:"score"`
					Matched []string `json:"matched"`
				}
				rows := make([]row, 0, len(hits))
				for _, h := range hits {
					rows = append(rows, row{summarize(h.Tool), h.Score, h.Matched})
				}
				return writeJSON(a.stdout, map[string]any{"query": strings.Join(args, " "), "results": rows})
			}
			if len(hits) == 0 {
				fmt.Fprintln(a.stdout, "no matching tools")
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TOOL\tVERSION\tDESCRIPTION")
			for _, h := range hits {
				m := h.Tool.Manifest
				fmt.Fprintf(w, "%s\t%s\t%s\n", h.Tool.Ref(), m.Version, m.Description)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&limit, "limit", 10, "max results (0 = all)")
	return cmd
}
