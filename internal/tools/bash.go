package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// bashTool executes a shell command. Unix-only in v1 (process group via setpgid);
// Windows support is a documented follow-up.
type bashTool struct{}

type bashParams struct {
	Command string `json:"command" jsonschema:"description=Bash command to execute"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds (optional, no default timeout)"`
}

func (bashTool) Name() string { return "bash" }

func (bashTool) Description() string {
	return "Execute a bash command in the current working directory. Returns combined stdout and stderr. Optionally provide a timeout in seconds."
}

func (bashTool) Schema() map[string]any { return schemaFor(&bashParams{}) }

func (bashTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	var p bashParams
	if err := decodeArgs(args, &p); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	if p.Command == "" {
		return "command is required", true
	}

	runCtx := ctx
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(p.Timeout)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, "bash", "-c", p.Command)
	// Run in its own process group so children are cleaned up on cancel/timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Kill the whole process group (negative pid).
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	out, err := cmd.CombinedOutput()
	result := string(out)

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return result + fmt.Sprintf("\n[timed out after %ds]", p.Timeout), true
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return result + fmt.Sprintf("\n[exit code %d]", exitErr.ExitCode()), true
		}
		return result + "\n" + err.Error(), true
	}
	return result, false
}
