package ui

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/runner"
	"github.com/nxnom/maggot/internal/toolbox"
)

type RunStatus string

const (
	StatusRunning   RunStatus = "running"
	StatusSucceeded RunStatus = "succeeded"
	StatusFailed    RunStatus = "failed"
	StatusCancelled RunStatus = "cancelled"
)

// RunMeta is saved as run.json in the run folder.
type RunMeta struct {
	ID          string         `json:"id"`
	Ref         string         `json:"ref"`
	ToolID      string         `json:"tool_id"`
	ToolName    string         `json:"tool_name"`
	ToolVersion string         `json:"tool_version"`
	Values      map[string]any `json:"values,omitempty"`
	Argv        []string       `json:"argv"`
	Dir         string         `json:"dir"`
	OutDir      string         `json:"out_dir"`
	OutputKind  string         `json:"output_kind,omitempty"`
	Format      string         `json:"format,omitempty"`
	Status      RunStatus      `json:"status"`
	ExitCode    *int           `json:"exit_code,omitempty"`
	Error       string         `json:"error,omitempty"`
	Started     time.Time      `json:"started"`
	Ended       *time.Time     `json:"ended,omitempty"`
	// Progress is 0..1, or -1 when the tool reports none.
	Progress float64 `json:"progress"`
}

// Message is one item of a run's live stream.
type Message struct {
	Seq int `json:"seq"`
	// Type: out, err (output lines), progress, event (from MAGGOT_EVENTS), status.
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Value  float64         `json:"value,omitempty"`
	Event  json.RawMessage `json:"event,omitempty"`
	Status *RunMeta        `json:"status,omitempty"`
}

const (
	maxHistory    = 5000
	maxStdoutFile = 10 << 20
	maxLine       = 64 << 10
)

// Run is one execution of a tool.
type Run struct {
	mu      sync.Mutex
	meta    RunMeta
	history []Message
	seq     int
	subs    map[chan Message]struct{}
	cmd     *exec.Cmd
	done    chan struct{}
	logFile *os.File
	stdout  *os.File
	stdoutN int64
	cancel  bool
	re      *regexp.Regexp
	max     float64
}

// Meta returns a copy of the run metadata.
func (r *Run) Meta() RunMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.meta
}

// Done is closed when the process has exited and the run is saved.
func (r *Run) Done() <-chan struct{} { return r.done }

func (r *Run) publish(m Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	m.Seq = r.seq
	if m.Type == "out" || m.Type == "err" {
		if r.logFile != nil {
			b, _ := json.Marshal(map[string]string{"t": m.Type, "s": m.Text})
			r.logFile.Write(append(b, '\n'))
		}
	}
	if m.Type == "progress" {
		r.meta.Progress = m.Value
	}
	r.history = append(r.history, m)
	if len(r.history) > maxHistory {
		r.history = r.history[len(r.history)-maxHistory:]
	}
	for ch := range r.subs {
		select {
		case ch <- m:
		default: // slow reader: it will see the final status from Meta
		}
	}
}

// Subscribe returns the messages so far and a channel for new ones. Call
// the returned func to stop.
func (r *Run) Subscribe() ([]Message, <-chan Message, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan Message, 512)
	r.subs[ch] = struct{}{}
	hist := append([]Message{}, r.history...)
	return hist, ch, func() {
		r.mu.Lock()
		delete(r.subs, ch)
		r.mu.Unlock()
	}
}

// Manager starts runs and finds old ones on disk.
type Manager struct {
	Root string // <data dir>/runs
	Keep int

	mu     sync.Mutex
	active map[string]*Run
}

func NewManager(root string, keep int) *Manager {
	if keep <= 0 {
		keep = config.DefaultRunsKeep
	}
	return &Manager{Root: root, Keep: keep, active: map[string]*Run{}}
}

var runIDPattern = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{6}$`)

func newRunID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// Start validates values, creates the run folder and starts the tool.
// The tool runs inside <run>/out, which is also passed as outputs.dir_flag.
func (m *Manager) Start(t *toolbox.Tool, values map[string]any, extraEnv []string) (*Run, error) {
	man := t.Manifest
	id := newRunID()
	dir := filepath.Join(m.Root, id)
	out := filepath.Join(dir, "out")
	outFlagDir := ""
	if man.Outputs != nil && man.Outputs.DirFlag != "" {
		outFlagDir = out
	}
	args, err := runner.BuildArgs(man, values, outFlagDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	cmd, err := runner.Command(t.Dir, man, args, runner.Options{
		Dir: out, ExtraEnv: extraEnv, EventsPath: filepath.Join(dir, "events.ndjson"),
	})
	if err != nil {
		return nil, err
	}
	setGroup(cmd)
	r := &Run{
		meta: RunMeta{
			ID: id, Ref: t.Ref(), ToolID: man.ID, ToolName: man.Name, ToolVersion: man.Version,
			Values: values, Argv: cmd.Args, Dir: dir, OutDir: out, Status: StatusRunning,
			Started: time.Now(), Progress: -1,
		},
		subs: map[chan Message]struct{}{},
		cmd:  cmd,
		done: make(chan struct{}),
	}
	if man.Outputs != nil {
		r.meta.OutputKind, r.meta.Format = man.Outputs.Kind, man.Outputs.Format
		if p := man.Outputs.Progress; p != nil {
			r.re = regexp.MustCompile(p.Regex) // validated with the manifest
			r.max = p.Max
		}
	}
	if r.max == 0 {
		r.max = 100
	}
	if r.logFile, err = os.Create(filepath.Join(dir, "log.ndjson")); err != nil {
		return nil, err
	}
	if r.stdout, err = os.Create(filepath.Join(dir, "stdout.txt")); err != nil {
		return nil, err
	}
	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()
	cmd.Stdin = nil
	if err := m.saveMeta(r.meta); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		r.logFile.Close()
		r.stdout.Close()
		os.RemoveAll(dir)
		return nil, fmt.Errorf("start %s: %w", cmd.Args[0], err)
	}
	m.mu.Lock()
	m.active[id] = r
	m.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.readLines(stdoutPipe, "out") }()
	go func() { defer wg.Done(); r.readLines(stderrPipe, "err") }()
	stopTail := make(chan struct{})
	tailDone := make(chan struct{})
	go func() { r.tailEvents(filepath.Join(dir, "events.ndjson"), stopTail); close(tailDone) }()

	go func() {
		wg.Wait()
		waitErr := cmd.Wait()
		close(stopTail)
		<-tailDone
		r.finish(waitErr)
		m.saveMeta(r.Meta())
		r.publish(Message{Type: "status", Status: ptr(r.Meta())})
		close(r.done)
		m.mu.Lock()
		delete(m.active, id)
		m.mu.Unlock()
		m.prune(man.ID)
	}()
	r.publish(Message{Type: "status", Status: ptr(r.Meta())})
	return r, nil
}

func ptr[T any](v T) *T { return &v }

func (r *Run) finish(waitErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logFile.Close()
	r.stdout.Close()
	now := time.Now()
	r.meta.Ended = &now
	code := 0
	var ee *exec.ExitError
	switch {
	case errors.As(waitErr, &ee):
		code = ee.ExitCode()
	case waitErr != nil:
		r.meta.Error = waitErr.Error()
		code = -1
	}
	r.meta.ExitCode = &code
	switch {
	case r.cancel:
		r.meta.Status = StatusCancelled
	case code == 0:
		r.meta.Status = StatusSucceeded
		if r.meta.Progress >= 0 {
			r.meta.Progress = 1
		}
	default:
		r.meta.Status = StatusFailed
	}
}

// splitLines splits on \n, \r\n and lone \r (progress bars redraw with \r).
func splitLines(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		adv := i + 1
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			adv++
		} else if data[i] == '\r' && i+1 == len(data) && !atEOF {
			return 0, nil, nil // maybe \r\n split across reads
		}
		return adv, data[:i], nil
	}
	if len(data) >= maxLine || (atEOF && len(data) > 0) {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func (r *Run) readLines(rd io.Reader, kind string) {
	if kind == "out" {
		rd = io.TeeReader(rd, writerFunc(func(p []byte) (int, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.stdoutN < maxStdoutFile {
				n, _ := r.stdout.Write(p)
				r.stdoutN += int64(n)
			}
			return len(p), nil
		}))
	}
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine+1)
	sc.Split(splitLines)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		r.publish(Message{Type: kind, Text: line})
		if r.re != nil {
			if mm := r.re.FindStringSubmatch(line); len(mm) > 1 {
				if v, err := strconv.ParseFloat(mm[1], 64); err == nil {
					r.publish(Message{Type: "progress", Value: clamp(v / r.max)})
				}
			}
		}
	}
	io.Copy(io.Discard, rd) // drain after an over-long line
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// tailEvents follows the MAGGOT_EVENTS file until stop is closed, then reads
// what is left.
func (r *Run) tailEvents(path string, stop <-chan struct{}) {
	var f *os.File
	var rd *bufio.Reader
	var partial []byte
	read := func() {
		if f == nil {
			var err error
			if f, err = os.Open(path); err != nil {
				return
			}
			rd = bufio.NewReader(f)
		}
		for {
			chunk, err := rd.ReadBytes('\n')
			partial = append(partial, chunk...)
			if err != nil {
				return // wait for more
			}
			r.handleEvent(bytes.TrimSpace(partial))
			partial = partial[:0]
		}
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			read()
		case <-stop:
			read()
			if len(bytes.TrimSpace(partial)) > 0 {
				r.handleEvent(bytes.TrimSpace(partial))
			}
			if f != nil {
				f.Close()
			}
			return
		}
	}
}

func (r *Run) handleEvent(line []byte) {
	if len(line) == 0 || !json.Valid(line) {
		return
	}
	var ev struct {
		Type  string   `json:"type"`
		Value *float64 `json:"value"`
	}
	json.Unmarshal(line, &ev)
	r.publish(Message{Type: "event", Event: json.RawMessage(append([]byte{}, line...))})
	if ev.Type == "progress" && ev.Value != nil {
		r.publish(Message{Type: "progress", Value: clamp(*ev.Value)})
	}
}

func (m *Manager) saveMeta(meta RunMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(meta.Dir, "run.json"), data, 0o644)
}

// Active returns a running run.
func (m *Manager) Active(id string) (*Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.active[id]
	return r, ok
}

// Get returns the metadata of a run, running or finished.
func (m *Manager) Get(id string) (RunMeta, error) {
	if r, ok := m.Active(id); ok {
		return r.Meta(), nil
	}
	if !runIDPattern.MatchString(id) {
		return RunMeta{}, fs.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(m.Root, id, "run.json"))
	if err != nil {
		return RunMeta{}, err
	}
	var meta RunMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return RunMeta{}, err
	}
	if meta.Status == StatusRunning { // maggot ui stopped while it ran
		meta.Status = StatusFailed
		meta.Error = "maggot ui stopped while the run was active"
	}
	meta.Dir = filepath.Join(m.Root, id) // trust the location, not the file
	meta.OutDir = filepath.Join(meta.Dir, "out")
	return meta, nil
}

// List returns runs for a tool id (all tools when empty), newest first.
func (m *Manager) List(toolID string, limit int) []RunMeta {
	entries, _ := os.ReadDir(m.Root)
	var out []RunMeta
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if !e.IsDir() || !runIDPattern.MatchString(e.Name()) {
			continue
		}
		meta, err := m.Get(e.Name())
		if err != nil || (toolID != "" && meta.ToolID != toolID) {
			continue
		}
		out = append(out, meta)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Log replays the saved output lines of a finished run.
func (m *Manager) Log(id string) ([]Message, error) {
	if !runIDPattern.MatchString(id) {
		return nil, fs.ErrNotExist
	}
	f, err := os.Open(filepath.Join(m.Root, id, "log.ndjson"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var l struct{ T, S string }
		if json.Unmarshal(sc.Bytes(), &l) == nil {
			out = append(out, Message{Seq: len(out) + 1, Type: l.T, Text: l.S})
		}
	}
	if len(out) > maxHistory {
		out = out[len(out)-maxHistory:]
	}
	return out, nil
}

// Cancel stops a running run.
func (m *Manager) Cancel(id string) error {
	r, ok := m.Active(id)
	if !ok {
		return errors.New("run is not active")
	}
	r.mu.Lock()
	r.cancel = true
	r.mu.Unlock()
	go stopGroup(r.cmd, r.done)
	return nil
}

// StopAll cancels every active run and waits for them (used on shutdown,
// because runs live in their own process groups and do not get Ctrl-C).
func (m *Manager) StopAll(timeout time.Duration) {
	m.mu.Lock()
	runs := make([]*Run, 0, len(m.active))
	for _, r := range m.active {
		runs = append(runs, r)
	}
	m.mu.Unlock()
	for _, r := range runs {
		m.Cancel(r.Meta().ID)
	}
	deadline := time.After(timeout)
	for _, r := range runs {
		select {
		case <-r.Done():
		case <-deadline:
			return
		}
	}
}

// Delete removes a finished run folder.
func (m *Manager) Delete(id string) error {
	if _, ok := m.Active(id); ok {
		return errors.New("cancel the run before deleting it")
	}
	if !runIDPattern.MatchString(id) {
		return fs.ErrNotExist
	}
	return os.RemoveAll(filepath.Join(m.Root, id))
}

// DeleteAll removes every finished run of a tool and returns how many.
func (m *Manager) DeleteAll(toolID string) int {
	n := 0
	for _, r := range m.List(toolID, 0) {
		if _, running := m.Active(r.ID); running {
			continue
		}
		if os.RemoveAll(filepath.Join(m.Root, r.ID)) == nil {
			n++
		}
	}
	return n
}

// prune keeps the newest m.Keep finished runs of a tool.
func (m *Manager) prune(toolID string) {
	runs := m.List(toolID, 0)
	kept := 0
	for _, r := range runs {
		if r.Status == StatusRunning {
			continue
		}
		kept++
		if kept > m.Keep {
			os.RemoveAll(filepath.Join(m.Root, r.ID))
		}
	}
}

// OutputFile is a file the run produced.
type OutputFile struct {
	Path  string    `json:"path"` // relative to out dir, slash separated
	Size  int64     `json:"size"`
	Mime  string    `json:"mime"`
	MTime time.Time `json:"mtime"`
}

// Outputs lists files in the run's out folder (newest first, max 500).
func (m *Manager) Outputs(meta RunMeta) []OutputFile {
	out := []OutputFile{}
	filepath.WalkDir(meta.OutDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(out) >= 500 || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(meta.OutDir, p)
		out = append(out, OutputFile{Path: filepath.ToSlash(rel), Size: info.Size(), Mime: mimeOf(p), MTime: info.ModTime()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].MTime.After(out[j].MTime) })
	return out
}

func mimeOf(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	extra := map[string]string{".md": "text/markdown", ".mkv": "video/x-matroska", ".m4a": "audio/mp4",
		".webm": "video/webm", ".opus": "audio/opus", ".flac": "audio/flac", ".heic": "image/heic"}
	if t, ok := extra[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return strings.SplitN(t, ";", 2)[0]
	}
	return "application/octet-stream"
}

// stdoutText returns the saved stdout of a run (capped).
func (m *Manager) stdoutText(meta RunMeta) string {
	b, _ := os.ReadFile(filepath.Join(meta.Dir, "stdout.txt"))
	return string(b)
}
