// Package warm keeps tools loaded in memory between runs.
//
// A tool that declares "warm" in tool.json has a serve mode: started with
// warm.args it loads once (for example a model), listens on a Unix socket and
// exits after an idle time. Normal runs of the tool forward their job to that
// socket when it answers, and fall back to a normal (cold) run when it does
// not, so the tool still works without maggot.
//
// Socket protocol (JSON lines):
//
//	→ {"type":"status"}
//	← {"ok":true,"idle_left":540,"jobs":3,"busy":false}
//
//	→ {"type":"run","argv":[...],"cwd":"/path","env":{"MAGGOT_EVENTS":"..."}}
//	← {"out":"text"} / {"err":"text"} … then {"exit":0}
package warm

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/runner"
	"github.com/nxnom/maggot/internal/toolbox"
)

// Env vars given to a tool.
const (
	EnvSocket = "MAGGOT_WARM_SOCKET"
	EnvIdle   = "MAGGOT_WARM_IDLE"
)

// DefaultIdle is how long a warm tool waits for a job before it exits.
const DefaultIdle = 10 * time.Minute

// Record is saved as <data>/warm/<id>.json while a tool is warm.
type Record struct {
	ID      string        `json:"id"`
	Ref     string        `json:"ref"`
	PID     int           `json:"pid"`
	Socket  string        `json:"socket"`
	Idle    time.Duration `json:"idle"`
	Started time.Time     `json:"started"`
	Log     string        `json:"log"`
}

// Status is what `maggot warm list` and the UI show.
type Status struct {
	Record
	Running  bool    `json:"running"`
	IdleLeft float64 `json:"idle_left"` // seconds until it stops without jobs
	Jobs     int     `json:"jobs"`
	Busy     bool    `json:"busy"`
}

func dir() (string, error) {
	d, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "warm"), nil
}

// SocketPath is where tool id's warm process listens. Unix socket paths are
// limited to about 100 bytes, so long paths move to a short folder in the
// system temp folder.
func SocketPath(id string) (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, id+".sock")
	if len(p) <= 100 {
		return p, nil
	}
	sum := sha256.Sum256([]byte(p))
	short := filepath.Join(os.TempDir(), "maggot-"+strconv.Itoa(os.Getuid()))
	if len(short) > 60 {
		short = filepath.Join("/tmp", "maggot-"+strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(short, hex.EncodeToString(sum[:6])+".sock"), nil
}

// Env returns the env var that lets a run find the warm process. It is safe
// to set even when the tool is not warm: the tool then runs cold.
func Env(t *toolbox.Tool) []string {
	if t.Manifest.Warm == nil {
		return nil
	}
	p, err := SocketPath(t.Manifest.ID)
	if err != nil {
		return nil
	}
	return []string{EnvSocket + "=" + p}
}

// Start launches the tool's serve mode and waits until it answers (loading
// can take a while) or wait passes.
func Start(t *toolbox.Tool, idle, wait time.Duration, extraEnv []string) (*Status, error) {
	rec, exited, err := StartAsync(t, idle, extraEnv)
	if err != nil {
		return nil, err
	}
	deadline := time.After(wait)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-exited:
			remove(rec.ID)
			return nil, fmt.Errorf("%s stopped while loading:\n%s", t.Ref(), tail(rec.Log, 20))
		case <-deadline:
			return &Status{Record: *rec}, fmt.Errorf("%s is still loading after %s; check `maggot warm list` (log: %s)", t.Ref(), wait, rec.Log)
		case <-tick.C:
			if st, err := query(rec.Socket); err == nil {
				st.Record = *rec
				return st, nil
			}
		}
	}
}

// StartAsync launches the tool's serve mode in the background and returns at
// once. exited is closed if the process ends.
func StartAsync(t *toolbox.Tool, idle time.Duration, extraEnv []string) (*Record, <-chan struct{}, error) {
	m := t.Manifest
	if m.Warm == nil {
		return nil, nil, fmt.Errorf("%s cannot stay warm (no \"warm\" in tool.json)", t.Ref())
	}
	if st, _ := Get(m.ID); st != nil {
		if st.Running {
			return nil, nil, fmt.Errorf("%s is already warm; stop it first to change the time", t.Ref())
		}
		return nil, nil, fmt.Errorf("%s is already loading", t.Ref())
	}
	if idle <= 0 {
		idle = DefaultIdle
	}
	d, err := dir()
	if err != nil {
		return nil, nil, err
	}
	sock, err := SocketPath(m.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range []string{d, filepath.Dir(sock)} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			return nil, nil, err
		}
	}
	os.Remove(sock)
	logPath := filepath.Join(d, m.ID+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, nil, err
	}
	defer logFile.Close()
	cmd, err := runner.Command(t.Dir, m, m.Warm.Args, runner.Options{
		Dir: d,
		ExtraEnv: append(append([]string{}, extraEnv...),
			EnvSocket+"="+sock, EnvIdle+"="+strconv.Itoa(int(idle.Seconds()))),
	})
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start %s: %w", cmd.Args[0], err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }() // reaps the child in long-lived processes
	rec := Record{ID: m.ID, Ref: t.Ref(), PID: cmd.Process.Pid, Socket: sock, Idle: idle, Started: time.Now(), Log: logPath}
	if err := save(rec); err != nil {
		return nil, nil, err
	}
	return &rec, exited, nil
}

// WaitLoaded waits until the tool's warm process answers, it stops, stop is
// closed, or max passes. New lines of its log are passed to log, so a run can
// show the loading progress. It reports whether the process is ready.
func WaitLoaded(id string, max time.Duration, stop <-chan struct{}, log func(string)) bool {
	var offset int64
	logPath := ""
	flush := func() {
		if logPath == "" || log == nil {
			return
		}
		f, err := os.Open(logPath)
		if err != nil {
			return
		}
		defer f.Close()
		f.Seek(offset, 0)
		b, _ := io.ReadAll(f)
		// Only whole lines; progress bars redraw with \r, keep the last state.
		if i := strings.LastIndexAny(string(b), "\n"); i >= 0 {
			for _, line := range strings.Split(string(b[:i]), "\n") {
				if k := strings.LastIndex(line, "\r"); k >= 0 {
					line = line[k+1:]
				}
				if strings.TrimSpace(line) != "" {
					log(line)
				}
			}
			offset += int64(i + 1)
		}
	}
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		st, _ := Get(id)
		if st == nil {
			flush()
			return false
		}
		logPath = st.Log
		flush()
		if st.Running {
			return true
		}
		select {
		case <-stop:
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return false
}

// Get returns the status of a warm tool, or nil when it is not warm.
// Records of processes that are gone are removed.
func Get(id string) (*Status, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(d, id+".json"))
	if err != nil {
		return nil, nil
	}
	var rec Record
	if json.Unmarshal(b, &rec) != nil {
		remove(id)
		return nil, nil
	}
	st, err := query(rec.Socket)
	if err != nil {
		if !alive(rec.PID) {
			remove(id)
			return nil, nil
		}
		return &Status{Record: rec}, nil // still loading
	}
	st.Record = rec
	return st, nil
}

// Loading reports whether the tool's warm process is started but not ready.
func (s *Status) Loading() bool { return s != nil && !s.Running }

// WaitReady waits while the tool's warm process is still loading, so a run
// uses it instead of loading a second copy. It returns the status (nil when
// the tool is not warm) once it is ready, gone, or max has passed.
func WaitReady(id string, max time.Duration, onWait func()) *Status {
	st, _ := Get(id)
	if !st.Loading() {
		return st
	}
	if onWait != nil {
		onWait()
	}
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if st, _ = Get(id); !st.Loading() {
			return st
		}
	}
	return st
}

// List returns every warm tool.
func List() []Status {
	d, err := dir()
	if err != nil {
		return nil
	}
	entries, _ := os.ReadDir(d)
	var out []Status
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			if st, _ := Get(id); st != nil {
				out = append(out, *st)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Stop ends a warm tool.
func Stop(id string) error {
	d, err := dir()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(d, id+".json"))
	if err != nil {
		return fmt.Errorf("%s is not warm", id)
	}
	var rec Record
	json.Unmarshal(b, &rec)
	if rec.PID > 0 {
		terminate(rec.PID)
	}
	remove(id)
	os.Remove(rec.Socket)
	return nil
}

func query(sock string) (*Status, error) {
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(`{"type":"status"}` + "\n")); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var st Status
	if err := json.Unmarshal(line, &st); err != nil {
		return nil, err
	}
	st.Running = true
	return &st, nil
}

func save(rec Record) error {
	d, _ := dir()
	b, _ := json.MarshalIndent(rec, "", "  ")
	return config.WriteFileAtomic(filepath.Join(d, rec.ID+".json"), b, 0o600)
}

func remove(id string) {
	d, _ := dir()
	os.Remove(filepath.Join(d, id+".json"))
}

func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "  " + strings.Join(lines, "\n  ")
}
