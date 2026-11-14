package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/nxnom/maggot/internal/search"
	"github.com/nxnom/maggot/internal/testlog"
	"github.com/spf13/cobra"
)

func (a *app) searchCmd() *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "search <words...>",
		Short: "Find tools and skills by what they do (matches id, name, tags, description, inputs)",
		Long: `Find tools and skills by what they do. Tools are programs maggot runs
(maggot run). Skills are step-by-step instructions for agents, often using
several tools and scripts (maggot skill show).`,
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
			skills, _ := e.Set.Skills()
			skillHits := search.RankSkills(skills, strings.Join(args, " "))
			if limit > 0 && len(skillHits) > limit {
				skillHits = skillHits[:limit]
			}
			type skillRow struct {
				skillSummary
				Score   int      `json:"score"`
				Matched []string `json:"matched"`
			}
			skillRows := []skillRow{}
			for _, h := range skillHits {
				skillRows = append(skillRows, skillRow{a.summarizeSkill(e, h.Skill), h.Score, h.Matched})
			}
			query := strings.Join(args, " ")
			tested, _ := testlog.Load()
			// No word matched: show the most similar tools anyway (typos,
			// other word forms), clearly marked as not a direct match.
			var closest []toolSummary
			if len(hits) == 0 {
				for _, t := range search.Closest(tools, query, 3) {
					closest = append(closest, summarize(t, tested))
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
					rows = append(rows, row{summarize(h.Tool, tested), h.Score, h.Matched})
				}
				out := map[string]any{"query": query, "results": rows, "skills": skillRows}
				if len(hits) == 0 && len(skillHits) == 0 {
					if closest == nil {
						closest = []toolSummary{}
					}
					out["closest"] = closest
					out["hint"] = "no tool matched the words; check the closest tools, try other words (what the tool does, file formats), or build a new tool"
				}
				return writeJSON(a.stdout, out)
			}
			if len(skillRows) > 0 {
				w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "SKILL\tMATCHED\tDESCRIPTION")
				for _, r := range skillRows {
					fmt.Fprintf(w, "%s\t%s\t%s\n", r.Ref, strings.Join(r.Matched, ","), brief(r.Description, 80))
				}
				w.Flush()
				fmt.Fprintln(a.stdout, "(skills are steps for agents: read one with `maggot skill show <name>`)")
				fmt.Fprintln(a.stdout)
			}
			if len(hits) == 0 {
				fmt.Fprintln(a.stdout, "no matching tools")
				if len(closest) > 0 {
					fmt.Fprintln(a.stdout, "closest (not a direct match):")
					for _, c := range closest {
						fmt.Fprintf(a.stdout, "  %s  %s\n", c.Ref, brief(c.Description, 80))
					}
				}
				a.archivedHint(e)
				return nil
			}
			w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TOOL\tMATCHED\tDESCRIPTION")
			for _, h := range hits {
				fmt.Fprintf(w, "%s\t%s\t%s\n", h.Tool.Ref(), strings.Join(h.Matched, ","), brief(h.Tool.Manifest.Description, 80))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			a.archivedHint(e)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&limit, "limit", 10, "max results (0 = all)")
	return cmd
}
