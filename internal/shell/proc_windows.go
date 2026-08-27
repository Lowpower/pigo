//go:build windows

package shell

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// Prepare starts a new Windows process group so cancel can taskkill the tree.
func Prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

// PrepareContext also sets cmd.Cancel; cmd must come from CommandContext.
func PrepareContext(cmd *exec.Cmd) {
	Prepare(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return KillTree(cmd.Process.Pid)
	}
}

// KillTree uses taskkill /T to stop pid and its children.
func KillTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	root := getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	exe := filepath.Join(root, "System32", "taskkill.exe")
	c := exec.Command(exe, "/F", "/T", "/PID", strconv.Itoa(pid))
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return c.Run()
}
