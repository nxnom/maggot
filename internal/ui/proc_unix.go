//go:build !windows

package ui

import (
	"os/exec"
	"syscall"
	"time"
)

// setGroup starts the child in its own process group so cancel can stop the
// tool and everything it started (yt-dlp → ffmpeg).
func setGroup(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// processAlive reports whether a process with this pid exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// stopGroup sends SIGTERM to the group, then SIGKILL if it is still running.
func stopGroup(c *exec.Cmd, done <-chan struct{}) {
	if c.Process == nil {
		return
	}
	pgid := -c.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(pgid, syscall.SIGKILL)
	}
}

// detach starts a process in its own session: no terminal, so closing the
// terminal or Ctrl-C in it does not reach the process.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
