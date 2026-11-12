package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/manifest"
	"github.com/spf13/cobra"
)

// cleanItem is one folder `maggot clean` can delete.
type cleanItem struct {
	Kind  string    `json:"kind"` // runs, drafts, uploads
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	Tool  string    `json:"tool,omitempty"`
	Bytes int64     `json:"bytes"`
	MTime time.Time `json:"mtime"`
}

func (a *app) cleanCmd() *cobra.Command {
	var yes, dryRun, asJSON bool
	var tool, olderThan string
	cmd := &cobra.Command{
		Use:   "clean [runs|drafts|uploads|all] [draft-id...]",
		Short: "Delete leftovers: web UI runs, drafts and uploaded files",
		Long: `Without arguments, show what maggot keeps in its data folder and how much
space it uses. With a kind, delete it (asks first; --yes to skip):

  runs     web UI run folders and their output files
  drafts   tool and skill drafts (all, or the ids you name)
  uploads  files dropped into the web UI
  all      all of the above

Toolboxes and your projects are never touched. Runs still in progress are kept.`,
		Example: `  maggot clean
  maggot clean runs --older-than 7d
  maggot clean runs --tool media-download
  maggot clean drafts photo-tidy --yes`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var age time.Duration
			if olderThan != "" {
				d, err := parseAge(olderThan)
				if err != nil {
					return err
				}
				age = d
			}
			items, err := cleanItems()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return a.cleanReport(items, asJSON)
			}
			kind := args[0]
			ids := args[1:]
			switch kind {
			case "runs", "drafts", "uploads", "all":
			default:
				return fmt.Errorf("unknown kind %q (choose runs, drafts, uploads or all)", kind)
			}
			if len(ids) > 0 && kind != "drafts" {
				return errors.New("ids can only be named for drafts")
			}
			var pick []cleanItem
			for _, it := range items {
				if kind != "all" && it.Kind != kind {
					continue
				}
				if tool != "" && it.Kind == "runs" && it.Tool != tool {
					continue
				}
				if tool != "" && it.Kind != "runs" {
					continue
				}
				if age > 0 && time.Since(it.MTime) < age {
					continue
				}
				if len(ids) > 0 && !contains(ids, it.Name) {
					continue
				}
				pick = append(pick, it)
			}
			if len(pick) == 0 {
				fmt.Fprintln(a.stdout, "nothing to delete")
				return nil
			}
			var total int64
			for _, it := range pick {
				total += it.Bytes
			}
			if dryRun {
				for _, it := range pick {
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", it.Kind, humanBytes(it.Bytes), it.Path)
				}
				fmt.Fprintf(a.stdout, "would delete %d folders, %s\n", len(pick), humanBytes(total))
				return nil
			}
			if err := a.confirm(yes, fmt.Sprintf("delete %d %s folders (%s)?", len(pick), kind, humanBytes(total))); err != nil {
				return err
			}
			deleted := 0
			for _, it := range pick {
				if os.RemoveAll(it.Path) == nil {
					deleted++
				}
			}
			fmt.Fprintf(a.stdout, "deleted %d folders, %s\n", deleted, humanBytes(total))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "only list what would be deleted")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON (report only)")
	cmd.Flags().StringVar(&tool, "tool", "", "only runs of this tool id")
	cmd.Flags().StringVar(&olderThan, "older-than", "", "only items older than this, e.g. 12h, 7d")
	return cmd
}

func (a *app) cleanReport(items []cleanItem, asJSON bool) error {
	if asJSON {
		if items == nil {
			items = []cleanItem{}
		}
		return writeJSON(a.stdout, map[string]any{"items": items})
	}
	type sum struct {
		n     int
		bytes int64
	}
	sums := map[string]*sum{"runs": {}, "drafts": {}, "uploads": {}}
	for _, it := range items {
		sums[it.Kind].n++
		sums[it.Kind].bytes += it.Bytes
	}
	data, _ := config.DataDir()
	fmt.Fprintf(a.stdout, "maggot data folder: %s\n", data)
	for _, k := range []string{"runs", "drafts", "uploads"} {
		fmt.Fprintf(a.stdout, "  %-8s %4d  %s\n", k, sums[k].n, humanBytes(sums[k].bytes))
	}
	var draftNames []string
	for _, it := range items {
		if it.Kind == "drafts" {
			draftNames = append(draftNames, it.Name)
		}
	}
	if len(draftNames) > 0 {
		fmt.Fprintf(a.stdout, "drafts: %s\n", strings.Join(draftNames, ", "))
	}
	fmt.Fprintln(a.stdout, "delete with: maggot clean runs|drafts|uploads|all")
	return nil
}

// cleanItems lists run, draft and upload folders in the data folder.
func cleanItems() ([]cleanItem, error) {
	data, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	var out []cleanItem
	list := func(kind, dir string, keep func(name, path string) (string, bool)) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			tool, ok := keep(e.Name(), p)
			if !ok {
				continue
			}
			info, _ := e.Info()
			out = append(out, cleanItem{Kind: kind, Name: e.Name(), Path: p, Tool: tool, Bytes: dirSize(p), MTime: info.ModTime()})
		}
	}
	list("runs", filepath.Join(data, "runs"), func(name, p string) (string, bool) {
		var meta struct {
			ToolID  string    `json:"tool_id"`
			Status  string    `json:"status"`
			Started time.Time `json:"started"`
		}
		b, err := os.ReadFile(filepath.Join(p, "run.json"))
		if err == nil {
			json.Unmarshal(b, &meta)
		}
		// A run marked running in the last day may still be going in an open UI.
		if meta.Status == "running" && time.Since(meta.Started) < 24*time.Hour {
			return "", false
		}
		return meta.ToolID, true
	})
	skillDrafts, _ := config.SkillDraftsDir()
	list("drafts", skillDrafts, func(name, p string) (string, bool) {
		return "", agentskill.Exists(p)
	})
	drafts, _ := config.DraftsDir()
	list("drafts", drafts, func(name, p string) (string, bool) {
		_, err := os.Stat(filepath.Join(p, manifest.FileName))
		return "", err == nil
	})
	list("uploads", filepath.Join(data, "uploads"), func(string, string) (string, bool) { return "", true })
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, nil
}

func dirSize(p string) int64 {
	var n int64
	filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// parseAge accepts Go durations plus a "d" (days) suffix.
func parseAge(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid age %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid age %q (use e.g. 12h or 7d)", s)
	}
	return d, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
