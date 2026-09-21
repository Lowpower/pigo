// Package runtime is the agent engine used by TUI, print, json, and rpc modes.
package runtime

import (
	"context"
	"errors"
	"fmt"
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
	// Error is set when a user_bash handler failed closed (no local exec).
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
// A throwing or invalid user_bash handler fails closed: local bash is not run.
func (e *Engine) RunUserBash(ctx context.Context, command string, exclude bool, onChunk func(string)) BashResult {
	if e != nil {
		payload := map[string]any{
			"command":            command,
			"excludeFromContext": exclude,
			"cwd":                e.Opts.Cwd,
		}
		for _, h := range e.Hosts {
			if h == nil || !h.Subscribed("user_bash") {
				continue
			}
			res, err := h.QueryEventOrErr(ctx, "user_bash", payload)
			if err != nil {
				return userBashFail(fmt.Sprintf("user_bash: %v", err))
			}
			if res == nil {
				continue
			}
			got, err := parseUserBashOverride(res)
			if err != nil {
				return userBashFail(err.Error())
			}
			if got != nil {
				return *got
			}
		}
		cwd := e.Opts.Cwd
		extra := e.sessionToolEnv()
		return runBash(ctx, e.toolRunner, cwd, command, extra, onChunk)
	}
	return runBash(ctx, tools.NewHostRunner(), "", command, nil, onChunk)
}

func userBashFail(msg string) BashResult {
	code := 1
	return BashResult{Output: msg, ExitCode: &code, Error: msg}
}

const userBashInvalid = "invalid user_bash handler result: return undefined for local execution or exactly one valid { operations } or { result } object"

func parseUserBashOverride(res map[string]any) (*BashResult, error) {
	if asBool(res["block"]) {
		reason := asString(res["reason"])
		if reason == "" {
			reason = "user_bash blocked"
		}
		return nil, errors.New(reason)
	}
	_, hasOps := res["operations"]
	_, hasResult := res["result"]
	if hasOps && hasResult {
		return nil, errors.New(userBashInvalid)
	}
	if hasOps {
		return nil, errors.New(userBashInvalid)
	}
	if !hasResult {
		return nil, errors.New(userBashInvalid)
	}
	r := asMap(res["result"])
	if r == nil {
		return nil, errors.New(userBashInvalid)
	}
	if _, ok := r["output"]; ok {
		out, ok := r["output"].(string)
		if !ok {
			return nil, errors.New(userBashInvalid)
		}
		cancelled, ok := r["cancelled"].(bool)
		if !ok {
			return nil, errors.New(userBashInvalid)
		}
		truncated, ok := r["truncated"].(bool)
		if !ok {
			return nil, errors.New(userBashInvalid)
		}
		br := BashResult{Output: out, Cancelled: cancelled, Truncated: truncated}
		if v, exists := r["exitCode"]; exists && v != nil {
			code, ok := intFromJSON(v)
			if !ok {
				return nil, errors.New(userBashInvalid)
			}
			br.ExitCode = &code
		}
		if v, exists := r["fullOutputPath"]; exists && v != nil {
			path, ok := v.(string)
			if !ok {
				return nil, errors.New(userBashInvalid)
			}
			br.FullOutputPath = path
		}
		return &br, nil
	}
	if _, hasStdout := r["stdout"]; hasStdout || r["stderr"] != nil || r["exitCode"] != nil {
		out := asString(r["stdout"])
		if errText := asString(r["stderr"]); errText != "" {
			if out != "" {
				out += "\n"
			}
			out += errText
		}
		code := 0
		if v, exists := r["exitCode"]; exists && v != nil {
			if n, ok := intFromJSON(v); ok {
				code = n
			}
		}
		return &BashResult{Output: out, ExitCode: &code}, nil
	}
	return nil, errors.New(userBashInvalid)
}

func intFromJSON(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
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
	if e == nil || e.Opts.Session == nil || result.Error != "" {
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
