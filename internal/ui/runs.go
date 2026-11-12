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
	"unicode/utf8"

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
	// Review marks a run whose files wait for the user's approval before an
	// agent moves them into a project (`maggot run --review`).
	Review bool `json:"review,omitempty"`
	// Origin is "ui" or "cli"; PID is the process that runs the tool.
	Origin string `json:"origin,omitempty"`
	PID    int    `json:"pid,omitempty"`
	// External means the tool wrote into a folder the user chose (OutDir),
	// not into the run folder. Those files belong to the user: deleting the
	// run never deletes them.
	External bool `json:"external,omitempty"`
	// Detached runs are owned by a background maggot process (PID), not by
	// the UI, so they keep going when the UI stops. Cancel writes a cancel
	// file in the run folder that the process watches.
	Detached bool       `json:"detached,omitempty"`
	ExitCode *int       `json:"exit_code,omitempty"`
	Error    string     `json:"error,omitempty"`
	Started  time.Time  `json:"started"`
	Ended    *time.Time `json:"ended,omitempty"`
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
	stop    chan struct{} // closed on cancel
	stopped sync.Once
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
	if r.logFile != nil {
		switch m.Type {
		case "out", "err":
			b, _ := json.Marshal(map[string]string{"t": m.Type, "s": m.Text})
			r.logFile.Write(append(b, '\n'))
		case "progress":
			b, _ := json.Marshal(map[string]any{"t": "p", "v": m.Value})
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
	return m.StartWith(t, StartOptions{Values: values, ExtraEnv: extraEnv, Origin: "ui"})
}

// StartOptions control a run.
type StartOptions struct {
	// Values are input values (turned into args with the manifest). Ignored
	// when Args is not nil.
	Values map[string]any
	// Args are raw tool args. The output folder flag is added in front unless
	// the args already contain it.
	Args     []string
	ExtraEnv []string
	// Cwd is the working directory; empty means the output folder.
	Cwd string
	// OutDir is a folder the user chose for the output. Empty means the
	// run's own out folder.
	OutDir string
	Review bool
	Origin string
	// ID is the run id to use; empty means a new one.
	ID string
	// Detached marks a run that a background maggot process owns, so it
	// keeps going when the UI stops (see worker.go).
	Detached bool
	// Before runs before the tool starts, while the run already shows as
	// running (for example: wait for a warm process to finish loading). log
	// adds a line to the run's output; stop is closed when the run is cancelled.
	Before func(log func(string), stop <-chan struct{})
}

// StartWith creates the run folder and starts the tool. Output goes to
// <run>/out through outputs.dir_flag.
func (m *Manager) StartWith(t *toolbox.Tool, o StartOptions) (*Run, error) {
	man := t.Manifest
	id := o.ID
	if id == "" {
		id = newRunID()
	} else if !runIDPattern.MatchString(id) {
		return nil, fmt.Errorf("bad run id %q", id)
	}
	dir := filepath.Join(m.Root, id)
	out := filepath.Join(dir, "out")
	external := false
	if o.OutDir != "" {
		abs, err := filepath.Abs(config.ExpandHome(o.OutDir))
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return nil, fmt.Errorf("output folder: %w", err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		out, external = abs, true
	}
	dirFlag := ""
	if man.Outputs != nil {
		dirFlag = man.Outputs.DirFlag
	}
	var args []string
	if o.Args != nil {
		args = o.Args
		if dirFlag != "" && !hasFlag(args, dirFlag) {
			args = append([]string{dirFlag, out}, args...)
		}
	} else {
		outFlagDir := ""
		if dirFlag != "" {
			outFlagDir = out
		}
		var err error
		if args, err = runner.BuildArgs(man, o.Values, outFlagDir); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	cwd := o.Cwd
	if cwd == "" {
		cwd = out
	}
	values := o.Values
	extraEnv := o.ExtraEnv
	cmd, err := runner.Command(t.Dir, man, args, runner.Options{
		Dir: cwd, ExtraEnv: extraEnv, EventsPath: filepath.Join(dir, "events.ndjson"),
	})
	if err != nil {
		return nil, err
	}
	setGroup(cmd)
	r := &Run{
		meta: RunMeta{
			ID: id, Ref: t.Ref(), ToolID: man.ID, ToolName: man.Name, ToolVersion: man.Version,
			Values: values, Argv: cmd.Args, Dir: dir, OutDir: out, Status: StatusRunning,
			Started: time.Now(), Progress: -1, Review: o.Review, Origin: o.Origin, PID: os.Getpid(), External: external,
			Detached: o.Detached,
		},
		subs: map[chan Message]struct{}{},
		cmd:  cmd,
		done: make(chan struct{}),
		stop: make(chan struct{}),
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
	if o.Before == nil {
		if err := cmd.Start(); err != nil {
			r.logFile.Close()
			r.stdout.Close()
			os.RemoveAll(dir)
			return nil, fmt.Errorf("start %s: %w", cmd.Args[0], err)
		}
		m.mu.Lock()
		m.active[id] = r
		m.mu.Unlock()
		m.follow(r, man.ID, stdoutPipe, stderrPipe)
		r.publish(Message{Type: "status", Status: ptr(r.Meta())})
		return r, nil
	}

	// Start later: first run Before (it may take minutes), then the tool.
	m.mu.Lock()
	m.active[id] = r
	m.mu.Unlock()
	r.publish(Message{Type: "status", Status: ptr(r.Meta())})
	go func() {
		o.Before(func(line string) { r.publish(Message{Type: "err", Text: line}) }, r.stop)
		var startErr error
		select {
		case <-r.stop:
			startErr = errCancelledBeforeStart
		default:
			startErr = cmd.Start()
		}
		if startErr != nil {
			if startErr != errCancelledBeforeStart {
				r.publish(Message{Type: "err", Text: fmt.Sprintf("start %s: %v", cmd.Args[0], startErr)})
			}
			r.finish(startErr)
			m.saveMeta(r.Meta())
			r.publish(Message{Type: "status", Status: ptr(r.Meta())})
			close(r.done)
			m.mu.Lock()
			delete(m.active, id)
			m.mu.Unlock()
			return
		}
		m.follow(r, man.ID, stdoutPipe, stderrPipe)
	}()
	return r, nil
}

var errCancelledBeforeStart = errors.New("cancelled before the tool started")

// follow reads the started tool's output and finishes the run when it exits.
func (m *Manager) follow(r *Run, toolID string, stdoutPipe, stderrPipe io.Reader) {
	id, dir, cmd := r.meta.ID, r.meta.Dir, r.cmd
	man := struct{ ID string }{toolID}
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
	// A run still marked running belongs to another maggot process (a CLI
	// review run, or another UI). If that process is gone, the run died.
	if meta.Status == StatusRunning && !processAlive(meta.PID) {
		meta.Status = StatusFailed
		meta.Error = "maggot stopped while the run was active"
	}
	meta.Dir = filepath.Join(m.Root, id) // trust the location, not the file
	if !meta.External {
		meta.OutDir = filepath.Join(meta.Dir, "out")
	}
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
	t, err := m.LogTail(id, maxHistory)
	if err != nil {
		return nil, err
	}
	return t.Lines, nil
}

// maxLineChars cuts very long output lines when they are shown. The log
// file keeps them whole.
const maxLineChars = 2000

func clipLine(s string) string {
	if len(s) <= maxLineChars {
		return s
	}
	cut := s[:maxLineChars]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return fmt.Sprintf("%s … (%d more characters)", cut, len(s)-len(cut))
}

// parseLogLine reads one log.ndjson line.
func parseLogLine(b []byte) (Message, bool) {
	var l struct {
		T, S string
		V    float64
	}
	if json.Unmarshal(b, &l) != nil {
		return Message{}, false
	}
	if l.T == "p" {
		return Message{Type: "progress", Value: l.V}, true
	}
	return Message{Type: l.T, Text: clipLine(l.S)}, true
}

// LogTail is the end of a run's log.
type LogTail struct {
	Lines    []Message `json:"lines"`    // the last output lines (out, err)
	Total    int       `json:"total"`    // output lines in the whole log
	Progress float64   `json:"progress"` // last progress value, -1 if none
	Offset   int64     `json:"-"`        // file offset after the last complete line
}

func (m *Manager) logPath(id string) (string, error) {
	if !runIDPattern.MatchString(id) {
		return "", fs.ErrNotExist
	}
	return filepath.Join(m.Root, id, "log.ndjson"), nil
}

// LogTail reads the last n output lines of a run's log. It reads the file
// once and keeps only n lines in memory, so huge logs stay cheap.
func (m *Manager) LogTail(id string, n int) (*LogTail, error) {
	p, err := m.logPath(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	t := &LogTail{Progress: -1}
	ring := make([]Message, 0, min(n, 1024))
	start := 0 // ring is full once len(ring) == n; start is the oldest line
	rd := bufio.NewReaderSize(f, 64<<10)
	for {
		b, err := rd.ReadBytes('\n')
		if err != nil {
			break // EOF, or a line still being written
		}
		t.Offset += int64(len(b))
		msg, ok := parseLogLine(b)
		if !ok {
			continue
		}
		if msg.Type == "progress" {
			t.Progress = msg.Value
			continue
		}
		t.Total++
		if n <= 0 {
			continue
		}
		if len(ring) < n {
			ring = append(ring, msg)
		} else {
			ring[start] = msg
			start = (start + 1) % n
		}
	}
	t.Lines = append(ring[start:], ring[:start]...)
	for i := range t.Lines {
		t.Lines[i].Seq = t.Total - len(t.Lines) + i + 1
	}
	return t, nil
}

// LogFrom reads the complete log lines written after offset off.
func (m *Manager) LogFrom(id string, off int64) ([]Message, int64, error) {
	p, err := m.logPath(id)
	if err != nil {
		return nil, off, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, off, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, off, err
	}
	var out []Message
	rd := bufio.NewReaderSize(f, 64<<10)
	for len(out) < maxHistory {
		b, err := rd.ReadBytes('\n')
		if err != nil {
			break
		}
		off += int64(len(b))
		if msg, ok := parseLogLine(b); ok {
			out = append(out, msg)
		}
	}
	return out, off, nil
}

// WriteLogText writes the whole log as plain text.
func (m *Manager) WriteLogText(id string, w io.Writer) error {
	p, err := m.logPath(id)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriterSize(w, 64<<10)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var l struct{ T, S string }
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.T == "p" {
			continue
		}
		bw.WriteString(l.S)
		bw.WriteByte('\n')
	}
	return bw.Flush()
}

// cancelFile is created in a detached run's folder to ask its process to
// stop the run.
const cancelFile = "cancel"

// Cancel stops a running run: one in this process, or a detached one that a
// background maggot process owns.
func (m *Manager) Cancel(id string) error {
	r, ok := m.Active(id)
	if !ok {
		meta, err := m.Get(id)
		if err != nil {
			return err
		}
		if meta.Status != StatusRunning {
			return errors.New("run is not active")
		}
		if !meta.Detached {
			return errors.New("this run belongs to another maggot command; stop it there")
		}
		return os.WriteFile(filepath.Join(meta.Dir, cancelFile), nil, 0o644)
	}
	r.mu.Lock()
	r.cancel = true
	r.mu.Unlock()
	r.stopped.Do(func() { close(r.stop) })
	go stopGroup(r.cmd, r.done)
	return nil
}

// StopAll cancels every run this process owns and waits for them. Those
// runs live in their own process groups and do not get Ctrl-C. Detached UI
// runs belong to their own process and are not affected.
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
	if meta, err := m.Get(id); err == nil && meta.Status == StatusRunning {
		return errors.New("cancel the run before deleting it")
	}
	return os.RemoveAll(filepath.Join(m.Root, id))
}

// DeleteAll removes every finished run of a tool and returns how many.
func (m *Manager) DeleteAll(toolID string) int {
	n := 0
	for _, r := range m.List(toolID, 0) {
		if _, running := m.Active(r.ID); running || r.Status == StatusRunning {
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

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// Take moves a finished run's output files into dest (keeping sub folders)
// and deletes the run, so only one copy remains. files limits it to some
// outputs (paths as listed by Outputs). Existing files in dest are not
// replaced unless overwrite is set.
func (m *Manager) Take(id, dest string, files []string, overwrite bool) ([]string, error) {
	meta, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	if meta.Status == StatusRunning {
		return nil, errors.New("the run is still active")
	}
	if meta.External {
		return nil, fmt.Errorf("the files are already in %s", meta.OutDir)
	}
	outputs := m.Outputs(meta)
	var pick []string
	if len(files) == 0 {
		for _, o := range outputs {
			pick = append(pick, o.Path)
		}
	} else {
		have := map[string]bool{}
		for _, o := range outputs {
			have[o.Path] = true
		}
		for _, f := range files {
			f = filepath.ToSlash(filepath.Clean(f))
			if !have[f] {
				return nil, fmt.Errorf("the run has no output %q", f)
			}
			pick = append(pick, f)
		}
	}
	if len(pick) == 0 {
		return nil, errors.New("the run has no output files")
	}
	dest, err = filepath.Abs(config.ExpandHome(dest))
	if err != nil {
		return nil, err
	}
	var conflicts []string
	for _, rel := range pick {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); err == nil {
			conflicts = append(conflicts, rel)
		}
	}
	if len(conflicts) > 0 && !overwrite {
		return nil, &ConflictError{Files: conflicts}
	}
	var moved []string
	for _, rel := range pick {
		src, err := insideDir(meta.OutDir, rel)
		if err != nil {
			return moved, err
		}
		dst := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return moved, err
		}
		if err := moveFile(src, dst); err != nil {
			return moved, err
		}
		moved = append(moved, dst)
	}
	return moved, os.RemoveAll(meta.Dir)
}

// ConflictError lists files that already exist in the destination.
type ConflictError struct{ Files []string }

func (e *ConflictError) Error() string {
	return "these files already exist in the destination: " + strings.Join(e.Files, ", ") + " (use overwrite to replace them)"
}

// moveFile renames, or copies and deletes when src and dst are on different disks.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// OutputFile is a file the run produced.
type OutputFile struct {
	Path  string    `json:"path"` // relative to out dir, slash separated
	Size  int64     `json:"size"`
	Mime  string    `json:"mime"`
	MTime time.Time `json:"mtime"`
}

// Outputs lists files in the run's out folder (newest first, max 500).
// For a user-chosen folder only files written since the run started count,
// so files that were already there are not shown as results.
func (m *Manager) Outputs(meta RunMeta) []OutputFile {
	out := []OutputFile{}
	since := meta.Started.Add(-2 * time.Second)
	seen := 0
	filepath.WalkDir(meta.OutDir, func(p string, d fs.DirEntry, err error) error {
		seen++
		if seen > 50000 {
			return filepath.SkipAll // a huge folder: stop looking
		}
		if err != nil || len(out) >= 500 || strings.HasPrefix(d.Name(), ".") {
			if d != nil && d.IsDir() && p != meta.OutDir {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		if meta.External && info.ModTime().Before(since) {
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
