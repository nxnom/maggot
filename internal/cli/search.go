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
			query := strings.Join(args, " ")
			// No word matched: show the most similar tools anyway (typos,
			// other word forms), clearly marked as not a direct match.
			var closest []toolSummary
			if len(hits) == 0 {
				for _, t := range search.Closest(tools, query, 3) {
					closest = append(closest, summarize(t))
				}
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
				out := map[string]any{"query": query, "results": rows}
				if len(hits) == 0 {
					if closest == nil {
						closest = []toolSummary{}
					}
					out["closest"] = closest
					out["hint"] = "no tool matched the words; check the closest tools, try other words (what the tool does, file formats), or build a new tool"
				}
				return writeJSON(a.stdout, out)
			}
			if len(hits) == 0 {
				fmt.Fprintln(a.stdout, "no matching tools")
				if len(closest) > 0 {
					fmt.Fprintln(a.stdout, "closest (not a direct match):")
					for _, c := range closest {
						fmt.Fprintf(a.stdout, "  %s  %s\n", c.Ref, c.Description)
					}
				}
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TOOL\tMATCHED\tDESCRIPTION")
			for _, h := range hits {
				fmt.Fprintf(w, "%s\t%s\t%s\n", h.Tool.Ref(), strings.Join(h.Matched, ","), h.Tool.Manifest.Description)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&limit, "limit", 10, "max results (0 = all)")
	return cmd
}
