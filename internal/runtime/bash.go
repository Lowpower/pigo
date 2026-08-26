// Package runtime is the agent engine used by TUI, print, json, and rpc modes.
package runtime

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"syscall"
)

// bashResult is the RPC bash payload (pi BashResult).
// packages/coding-agent/src/core/bash-executor.ts
type bashResult struct {
	Output         string
	ExitCode       *int
	Cancelled      bool
	Truncated      bool
	FullOutputPath string
}

func (r bashResult) asData() map[string]any {
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

func executeRPCBash(ctx context.Context, cwd, command string, onChunk func(string)) bashResult {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
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
		return bashResult{Output: err.Error(), ExitCode: &code}
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		code := 1
		return bashResult{Output: err.Error(), ExitCode: &code}
	}

	var output []byte
	r := bufio.NewReader(stdout)
	buf := make([]byte, 4096)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			output = append(output, buf[:n]...)
			if onChunk != nil {
				onChunk(chunk)
			}
		}
		if readErr != nil {
			break
		}
	}
	waitErr := cmd.Wait()
	cancelled := ctx.Err() != nil
	if cancelled {
		return bashResult{Output: string(output), Cancelled: true, Truncated: false}
	}
	code := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = 1
			if len(output) == 0 {
				return bashResult{Output: waitErr.Error(), ExitCode: &code}
			}
		}
	}
	return bashResult{Output: string(output), ExitCode: &code, Cancelled: false, Truncated: false}
}
