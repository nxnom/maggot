//go:build windows

package ui

import (
	"os/exec"
	"syscall"
)

func setGroup(c *exec.Cmd) {}

// processAlive cannot be checked cheaply on Windows; trust the run record.
func processAlive(pid int) bool { return pid > 0 }

func stopGroup(c *exec.Cmd, done <-chan struct{}) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detach starts a process without the UI's console, so it outlives the UI.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
