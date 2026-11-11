//go:build windows

package ui

import "os/exec"

func setGroup(c *exec.Cmd) {}

func stopGroup(c *exec.Cmd, done <-chan struct{}) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
