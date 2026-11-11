//go:build windows

package warm

import (
	"os"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func terminate(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}
