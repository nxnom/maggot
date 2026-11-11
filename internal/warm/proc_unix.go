//go:build !windows

package warm

import (
	"os/exec"
	"syscall"
)

// detach puts the warm process in its own session, so it outlives the
// maggot command that started it and does not get its Ctrl-C.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// terminate stops the warm process and anything it started.
func terminate(pid int) { _ = syscall.Kill(-pid, syscall.SIGTERM) }
