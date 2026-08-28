//go:build unix

package shell

import (
	"os/exec"
	"syscall"
)

// Prepare puts cmd in its own process group so cancel/timeout can kill children.
func Prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// PrepareContext also sets cmd.Cancel; cmd must come from CommandContext.
func PrepareContext(cmd *exec.Cmd) {
	Prepare(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}

// KillTree kills pid and its process group.
func KillTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}
