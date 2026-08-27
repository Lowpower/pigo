// Package runtime is the agent engine used by TUI, print, json, and rpc modes.
package runtime

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/Lowpower/pigo/internal/sandbox"
	"github.com/Lowpower/pigo/internal/shell"
	"github.com/Lowpower/pigo/internal/tools"
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

// RunBash executes command with the resolved shell in cwd (pi user bang / RPC bash).
func RunBash(ctx context.Context, cwd, command string, onChunk func(string)) BashResult {
	cfg, err := shell.GetConfig()
	if err != nil {
		code := 1
		return BashResult{Output: err.Error(), ExitCode: &code}
	}
	name, args := sandbox.Command(command, cwd, "")
	var cmd *exec.Cmd
	if name == "bwrap" {
		cmd = exec.CommandContext(ctx, name, args...)
		shell.PrepareContext(cmd)
	} else {
		cmd = shell.CommandContext(ctx, cfg, command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, waitErr := shell.WaitStream(cmd, onChunk)
	text, path, truncated := tools.BoundOutput(string(output), "pigo-bash")
	cancelled := ctx.Err() != nil
	if cancelled {
		return BashResult{Output: text, Cancelled: true, Truncated: truncated, FullOutputPath: path}
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
	return BashResult{Output: text, ExitCode: &code, Cancelled: false, Truncated: truncated, FullOutputPath: path}
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
