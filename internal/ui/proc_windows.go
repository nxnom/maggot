//go:build windows

package ui

import "os/exec"

func setGroup(c *exec.Cmd) {}

// processAlive cannot be checked cheaply on Windows; trust the run record.
func processAlive(pid int) bool { return pid > 0 }

func stopGroup(c *exec.Cmd, done <-chan struct{}) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
