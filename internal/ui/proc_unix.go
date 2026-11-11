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
