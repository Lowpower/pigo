// Package runtime is the agent engine used by TUI, print, json, and rpc modes.
package runtime

import (
	"context"
	"errors"
	"os/exec"
	"time"

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
	// Error is set when a user_bash handler failed or returned an illegal
	// payload. The local shell is not run.
	Error string
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

// RunUserBash runs bang/RPC bash, allowing extensions to replace the result.
// A handler error or any payload other than empty or a valid {result} stops
// the command. The local shell runs only when every handler leaves the
// payload empty.
func (e *Engine) RunUserBash(ctx context.Context, command string, exclude bool, onChunk func(string)) BashResult {
	if e == nil {
		return runBash(ctx, tools.NewHostRunner(), "", command, nil, onChunk)
	}
	payload := map[string]any{
		"command":            command,
		"excludeFromContext": exclude,
		"cwd":                e.Opts.Cwd,
	}
	for _, h := range e.Hosts {
		if h == nil || !h.Subscribed("user_bash") {
			continue
		}
		res, err := h.QueryEventErr(ctx, "user_bash", payload)
		if err != nil {
			return BashResult{Error: err.Error()}
		}
		local, parsed, perr := interpretUserBash(res)
		if perr != nil {
			return BashResult{Error: perr.Error()}
		}
		if !local {
			return parsed
		}
	}
	return runBash(ctx, e.toolRunner, e.Opts.Cwd, command, e.sessionToolEnv(), onChunk)
}

func interpretUserBash(payload map[string]any) (local bool, result BashResult, err error) {
	if len(payload) == 0 {
		return true, BashResult{}, nil
	}
	_, hasOps := payload["operations"]
	raw, hasResult := payload["result"]
	if hasOps == hasResult || hasOps {
		return false, BashResult{}, errors.New("invalid user_bash handler result")
	}
	m := asMap(raw)
	output, okOut := m["output"].(string)
	cancelled, okCancelled := m["cancelled"].(bool)
	truncated, okTruncated := m["truncated"].(bool)
	if m == nil || !okOut || !okCancelled || !okTruncated {
		return false, BashResult{}, errors.New("invalid user_bash handler result")
	}
	result = BashResult{Output: output, Cancelled: cancelled, Truncated: truncated}
	if v, ok := m["exitCode"]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			code := int(n)
			result.ExitCode = &code
		case int:
			code := n
			result.ExitCode = &code
		default:
			return false, BashResult{}, errors.New("invalid user_bash handler result")
		}
	}
	if path, ok := m["fullOutputPath"].(string); ok {
		result.FullOutputPath = path
	}
	return false, result, nil
}

// RunBash executes command with the resolved shell in cwd.
func RunBash(ctx context.Context, cwd, command string, onChunk func(string)) BashResult {
	return runBash(ctx, tools.NewHostRunner(), cwd, command, nil, onChunk)
}

func runBash(ctx context.Context, r tools.Runner, cwd, command string, extra map[string]string, onChunk func(string)) BashResult {
	if r == nil {
		r = tools.NewHostRunner()
	}
	cmd, err := r.Bash(ctx, command, cwd, extra)
	if err != nil {
		code := 1
		return BashResult{Output: err.Error(), ExitCode: &code}
	}
	if cwd != "" && cmd.Dir == "" {
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
