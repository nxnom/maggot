package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/runner"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/ui"
	"github.com/nxnom/maggot/internal/warm"
)

// runResult is the one JSON object `maggot run --json` prints, whatever the
// tool: status, the files it made, and what to do on failure.
type runResult struct {
	OK bool `json:"ok"`
	// Category: ok, invalid_input, missing_requirement, missing_env, output,
	// start_failed, tool_failed, cancelled.
	Category        string    `json:"category"`
	ExitCode        int       `json:"exit_code"`
	Ref             string    `json:"ref"`
	Command         string    `json:"command,omitempty"`
	DurationSeconds float64   `json:"duration_seconds"`
	OutputDir       string    `json:"output_dir,omitempty"`
	Files           []outFile `json:"files"`
	Stdout          string    `json:"stdout,omitempty"`
	StdoutTruncated bool      `json:"stdout_truncated,omitempty"`
	StderrTail      []string  `json:"stderr_tail,omitempty"`
	Problems        []Problem `json:"problems,omitempty"`
	Error           string    `json:"error,omitempty"`
	RunID           string    `json:"run_id,omitempty"`
	Review          bool      `json:"review,omitempty"`
	Next            []string  `json:"next,omitempty"`
}

type outFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

const stdoutCap = 256 << 10

// capBuffer keeps the first max bytes written to it.
type capBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if room := stdoutCap - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
			c.truncated = true
		} else {
			c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

// tailBuffer keeps the last n lines written to it (\r also ends a line, so
// progress bars collapse to their last state).
type tailBuffer struct {
	n     int
	lines []string
	part  string
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	s := t.part + string(p)
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	t.part = ""
	if len(s) > 0 && s[len(s)-1] != '\n' && s[len(s)-1] != '\r' && len(parts) > 0 {
		t.part, parts = parts[len(parts)-1], parts[:len(parts)-1]
	}
	for _, l := range parts {
		if strings.TrimSpace(l) != "" {
			t.lines = append(t.lines, l)
		}
	}
	if len(t.lines) > t.n {
		t.lines = t.lines[len(t.lines)-t.n:]
	}
	return len(p), nil
}

func (t *tailBuffer) result() []string {
	if strings.TrimSpace(t.part) != "" {
		return append(t.lines, t.part)
	}
	return t.lines
}

func (a *app) printResult(r runResult) error {
	if r.Files == nil {
		r.Files = []outFile{}
	}
	if err := writeJSON(a.stdout, r); err != nil {
		return err
	}
	if !r.OK {
		code := r.ExitCode
		if code == 0 {
			code = 1
		}
		return &exitError{code: code}
	}
	return nil
}

func (a *app) runJSON(ctx context.Context, t *toolbox.Tool, inputs string, raw []string, outDir string, review, skipChecks bool, events string) error {
	res := runResult{Ref: t.Ref()}
	var values map[string]any
	useInputs := inputs != ""
	if useInputs {
		if len(raw) > 0 {
			res.Category, res.Error = "invalid_input", "use either --inputs or raw tool args, not both"
			return a.printResult(res)
		}
		var err error
		if values, err = readInputs(inputs); err != nil {
			res.Category, res.Error = "invalid_input", err.Error()
			return a.printResult(res)
		}
	}
	prep := a.prepare(ctx, t, values, useInputs, raw, outDir, review)
	res.Command = prep.Command
	var blocking []Problem
	for _, p := range prep.Problems {
		if skipChecks && (p.Kind == "missing_requirement" || p.Kind == "missing_env") {
			continue
		}
		blocking = append(blocking, p)
	}
	if len(blocking) > 0 {
		res.Category, res.Problems = blocking[0].Kind, blocking
		for _, p := range blocking {
			if p.Fix != "" {
				res.Next = append(res.Next, p.Fix)
			}
		}
		return a.printResult(res)
	}

	if review {
		start := time.Now()
		meta, err := a.reviewRun(t, raw, values, true)
		res.Review, res.DurationSeconds = true, time.Since(start).Seconds()
		if meta == nil {
			res.Category, res.Error = "start_failed", errString(err)
			return a.printResult(res)
		}
		res.RunID, res.OutputDir = meta.ID, meta.OutDir
		res.Command = strings.Join(quoteAll(meta.Argv), " ")
		if meta.ExitCode != nil {
			res.ExitCode = *meta.ExitCode
		}
		if mgr, err := runManager(); err == nil {
			for _, f := range mgr.Outputs(*meta) {
				res.Files = append(res.Files, outFile{filepath.Join(meta.OutDir, filepath.FromSlash(f.Path)), f.Size})
			}
		}
		res.OK = meta.Status == ui.StatusSucceeded
		res.Category = map[ui.RunStatus]string{ui.StatusSucceeded: "ok", ui.StatusCancelled: "cancelled"}[meta.Status]
		if res.Category == "" {
			res.Category = "tool_failed"
		}
		if res.OK {
			link := "maggot ui"
			if base, token, ok := ui.Running(); ok {
				link = ui.ToolURL(base, token, t.Ref()) + "?run=" + meta.ID
			}
			res.Next = []string{
				"show the files to the user: " + link,
				fmt.Sprintf("approved: maggot take %s <destination folder>", meta.ID),
				fmt.Sprintf("rejected: maggot runs rm %s", meta.ID),
			}
		}
		return a.printResult(res)
	}

	extra, err := a.secretEnv(t)
	if err != nil {
		res.Category, res.Error = "missing_env", err.Error()
		return a.printResult(res)
	}
	extra = append(extra, warm.Env(t)...)
	a.waitForWarm(t)
	evPath := events
	if evPath == "" {
		if f, err := os.CreateTemp("", "maggot-events-*.ndjson"); err == nil {
			evPath = f.Name()
			f.Close()
			defer os.Remove(evPath)
		}
	}
	c, err := runner.Command(t.Dir, t.Manifest, prep.args, runner.Options{ExtraEnv: extra, EventsPath: evPath})
	if err != nil {
		res.Category, res.Error = "start_failed", err.Error()
		return a.printResult(res)
	}
	out := &capBuffer{}
	tail := &tailBuffer{n: 20}
	c.Stdin = a.stdin
	c.Stdout = io.MultiWriter(a.stderr, out)
	c.Stderr = io.MultiWriter(a.stderr, tail)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	start := time.Now()
	runErr := c.Run()
	res.DurationSeconds = time.Since(start).Seconds()

	var ee *exec.ExitError
	switch {
	case runErr == nil:
		res.OK, res.Category = true, "ok"
	case errors.As(runErr, &ee):
		res.ExitCode = ee.ExitCode()
		res.Category = "tool_failed"
		if res.ExitCode < 0 {
			res.ExitCode, res.Category = 130, "cancelled"
		}
	default:
		res.Category, res.Error, res.ExitCode = "start_failed", runErr.Error(), 1
	}
	res.Stdout, res.StdoutTruncated = out.buf.String(), out.truncated
	res.StderrTail = tail.result()
	res.OutputDir = prep.outDir
	res.Files = collectOutputs(prep.outDir, start, evPath)
	if t.Manifest.Outputs != nil && t.Manifest.Outputs.Kind == manifest.OutputFiles && res.Stdout != "" && len(res.Stdout) > 4096 {
		res.Stdout, res.StdoutTruncated = res.Stdout[:4096], true // files are the result; keep stdout short
	}
	if !res.OK && res.Category == "tool_failed" {
		res.Next = []string{
			"read stderr_tail for the tool's own error message",
			"check the inputs with: maggot prepare " + t.Ref() + " ...",
			"if the tool itself may be broken: maggot test " + t.Ref(),
		}
	}
	return a.printResult(res)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// collectOutputs finds the files a run made: files in the output folder
// changed since start, plus "output" events the tool reported.
func collectOutputs(dir string, start time.Time, eventsPath string) []outFile {
	seen := map[string]bool{}
	var out []outFile
	add := func(p string) {
		if abs, err := filepath.Abs(p); err == nil && !seen[abs] {
			if info, err := os.Stat(abs); err == nil && info.Mode().IsRegular() {
				seen[abs] = true
				out = append(out, outFile{abs, info.Size()})
			}
		}
	}
	since := start.Add(-time.Second)
	if dir != "" {
		count := 0
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			count++
			if err != nil || count > 20000 || len(out) >= 500 {
				return filepath.SkipAll
			}
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != dir {
				return filepath.SkipDir
			}
			if info, err := d.Info(); err == nil && !d.IsDir() && !info.ModTime().Before(since) {
				add(p)
			}
			return nil
		})
	}
	if eventsPath != "" {
		if f, err := os.Open(eventsPath); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var ev struct{ Type, Path string }
				if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Type == "output" && ev.Path != "" {
					add(ev.Path)
				}
			}
			f.Close()
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
