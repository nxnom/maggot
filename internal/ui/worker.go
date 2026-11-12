package ui

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/warm"
)

// Runs started from the UI are owned by a background maggot process (the
// "worker"), not by the UI server. The UI only starts it and then reads the
// run folder, like it does for runs of other maggot commands. So stopping
// the UI never stops a run: the UI is only a window onto what is running.
//
//	UI ──start──> maggot ui-run <spec> (own session) ──> tool
//	UI <──reads── run.json, log.ndjson                <── worker writes
//
// Cancel from the UI creates <run>/cancel; the worker sees it and stops the
// tool's process group.

// workerSpec is what the UI hands the worker. Secrets are not in it: they
// are in the worker's environment, which the tool inherits.
type workerSpec struct {
	ID          string         `json:"id"`
	Root        string         `json:"root"`
	Keep        int            `json:"keep"`
	ToolDir     string         `json:"tool_dir"`
	Handle      string         `json:"handle"`
	ToolboxPath string         `json:"toolbox_path"`
	Values      map[string]any `json:"values,omitempty"`
	OutDir      string         `json:"out_dir,omitempty"`
	// WaitWarm makes the run wait for the tool's warm process to load.
	WaitWarm bool `json:"wait_warm,omitempty"`
}

// workerReply is the one line the worker prints once the run has started.
type workerReply struct {
	Run   *RunMeta `json:"run,omitempty"`
	Error string   `json:"error,omitempty"`
}

// startDetached starts a run in a background maggot process and returns
// once the run folder exists.
func (s *Server) startDetached(t *toolbox.Tool, o StartOptions, waitWarm bool) (RunMeta, error) {
	spec := workerSpec{
		ID: newRunID(), Root: s.runs.Root, Keep: s.runs.Keep,
		ToolDir: t.Dir, Handle: t.Toolbox.Handle, ToolboxPath: t.Toolbox.Path,
		Values: o.Values, OutDir: o.OutDir, WaitWarm: waitWarm,
	}
	if err := os.MkdirAll(s.runs.Root, 0o755); err != nil {
		return RunMeta{}, err
	}
	specPath := filepath.Join(s.runs.Root, ".spec-"+spec.ID+".json")
	data, _ := json.Marshal(spec)
	if err := os.WriteFile(specPath, data, 0o600); err != nil {
		return RunMeta{}, err
	}
	cmd := exec.Command(s.Worker[0], append(append([]string{}, s.Worker[1:]...), specPath)...)
	cmd.Env = append(os.Environ(), o.ExtraEnv...)
	detach(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		os.Remove(specPath)
		return RunMeta{}, err
	}
	if err := cmd.Start(); err != nil {
		os.Remove(specPath)
		return RunMeta{}, fmt.Errorf("start the run process: %w", err)
	}
	// Reap the worker if it ends while the UI is still up.
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	line := make(chan []byte, 1)
	go func() {
		b, _ := bufio.NewReader(stdout).ReadBytes('\n')
		line <- b
	}()
	var b []byte
	select {
	case b = <-line:
	case <-time.After(30 * time.Second):
		return RunMeta{}, errors.New("the run process did not start in time")
	}
	var reply workerReply
	if err := json.Unmarshal(b, &reply); err != nil || (reply.Run == nil && reply.Error == "") {
		os.Remove(specPath)
		select {
		case err := <-waited:
			return RunMeta{}, fmt.Errorf("the run process stopped: %v", err)
		case <-time.After(time.Second):
		}
		return RunMeta{}, errors.New("the run process gave no answer")
	}
	if reply.Error != "" {
		return RunMeta{}, errors.New(reply.Error)
	}
	return *reply.Run, nil
}

// RunWorker runs one UI run to the end (the hidden `maggot ui-run` command).
// It prints one JSON line when the run has started, then waits.
func RunWorker(specPath string) error {
	out := os.Stdout
	reply := func(r workerReply) {
		b, _ := json.Marshal(r)
		out.Write(append(b, '\n'))
		out.Close() // the UI stops reading after this line
	}
	data, err := os.ReadFile(specPath)
	os.Remove(specPath)
	if err != nil {
		reply(workerReply{Error: err.Error()})
		return err
	}
	var spec workerSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		reply(workerReply{Error: "bad run spec: " + err.Error()})
		return err
	}
	m, err := manifest.Load(spec.ToolDir)
	if err != nil {
		reply(workerReply{Error: err.Error()})
		return err
	}
	t := &toolbox.Tool{Toolbox: &toolbox.Toolbox{Handle: spec.Handle, Path: spec.ToolboxPath}, Dir: spec.ToolDir, Manifest: m}
	mgr := NewManager(spec.Root, spec.Keep)
	opts := StartOptions{ID: spec.ID, Values: spec.Values, OutDir: spec.OutDir, Origin: "ui", Detached: true}
	if spec.WaitWarm {
		id := m.ID
		opts.Before = func(log func(string), stop <-chan struct{}) {
			log("Loading into memory. Later runs skip this while the tool stays warm.")
			if !warm.WaitLoaded(id, 15*time.Minute, stop, log) {
				log("The warm process is not ready, so this run loads the tool itself.")
			}
		}
	}
	// Stop signals cancel the run cleanly instead of killing the worker
	// and leaving the tool without an owner.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sig)

	run, err := mgr.StartWith(t, opts)
	if err != nil {
		reply(workerReply{Error: err.Error()})
		return err
	}
	meta := run.Meta()
	reply(workerReply{Run: &meta})

	cancelPath := filepath.Join(meta.Dir, cancelFile)
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	cancelled := false
	cancel := func() {
		if !cancelled {
			cancelled = true
			mgr.Cancel(meta.ID)
		}
	}
	for {
		select {
		case <-run.Done():
			os.Remove(cancelPath)
			return nil
		case <-sig:
			cancel()
		case <-tick.C:
			if _, err := os.Stat(cancelPath); err == nil {
				cancel()
			}
		}
	}
}
