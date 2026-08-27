// Package runtime is the agent engine used by TUI, print, json, and rpc modes.
package runtime

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/Lowpower/pigo/internal/sandbox"
)

// BashResult is the bang/RPC bash payload.
type BashResult struct {
	Output         string
	ExitCode       *int
	Cancelled      bool
	Truncated      bool
	FullOutputPath string
}

func (r BashResult) asData() map[string]any {
	data := map[string]any{
		"output":    r.Output,
		"cancelled": r.Cancelled,
		"truncated": r.Truncated,
	}
	if r.ExitCode != nil {
		data["exitCode"] = *r.ExitCode
	}
	if r.FullOutputPath != "" {
		data["fullOutputPath"] = r.FullOutputPath
	}
	return data
}

// RunBash executes command with bash -c in cwd (pi user bang / RPC bash).
func RunBash(ctx context.Context, cwd, command string, onChunk func(string)) BashResult {
	name, args := sandbox.Command(command, cwd, "")
	cmd := exec.CommandContext(ctx, name, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		code := 1
		return BashResult{Output: err.Error(), ExitCode: &code}
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		code := 1
		return BashResult{Output: err.Error(), ExitCode: &code}
	}

	var output []byte
	r := bufio.NewReader(stdout)
	buf := make([]byte, 4096)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			output = append(output, buf[:n]...)
			if onChunk != nil {
				onChunk(string(buf[:n]))
			}
		}
		if readErr != nil {
			break
		}
	}
	waitErr := cmd.Wait()
	cancelled := ctx.Err() != nil
	if cancelled {
		return BashResult{Output: string(output), Cancelled: true, Truncated: false}
	}
	code := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = 1
			if len(output) == 0 {
				return BashResult{Output: waitErr.Error(), ExitCode: &code}
			}
		}
	}
	return BashResult{Output: string(output), ExitCode: &code, Cancelled: false, Truncated: false}
}

// PersistBash writes a bashExecution session entry. excludeFromContext skips LLM history.
func (e *Engine) PersistBash(command string, result BashResult, exclude bool) {
	if e == nil || e.Opts.Session == nil {
		return
	}
	payload := map[string]any{
		"role":      "bashExecution",
		"command":   command,
		"output":    result.Output,
		"cancelled": result.Cancelled,
		"truncated": result.Truncated,
		"timestamp": time.Now().UnixMilli(),
	}
	if result.ExitCode != nil {
		payload["exitCode"] = *result.ExitCode
	}
	if result.FullOutputPath != "" {
		payload["fullOutputPath"] = result.FullOutputPath
	}
	if exclude {
		payload["excludeFromContext"] = true
	}
	_, _ = e.Opts.Session.AppendMessage("bashExecution", payload)
}
