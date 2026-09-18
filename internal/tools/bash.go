package tools

import (
	"context"
	"fmt"
)

// bashTool executes a shell command via shell.GetConfig (Git Bash / PATH / WSL).
type bashTool struct {
	prefix string
	cwd    string
	env    map[string]string
	envFn  func() map[string]string
	fs     Runner
}

type bashParams struct {
	Command string `json:"command" jsonschema:"description=Bash command to execute"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds (optional, no default timeout)"`
}

func (t bashTool) Name() string { return "bash" }

func (bashTool) Description() string {
	return fmt.Sprintf("Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.", DefaultMaxLines, DefaultMaxBytes/1024)
}

func (bashTool) Schema() map[string]any { return schemaFor(&bashParams{}) }

func (t bashTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	var p bashParams
	if err := decodeArgs(args, &p); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	if p.Command == "" {
		return "command is required", true
	}

	runCtx, cancel, errMsg := timeoutContext(ctx, p.Timeout)
	if errMsg != "" {
		return errMsg, true
	}
	if cancel != nil {
		defer cancel()
	}

	command := p.Command
	if t.prefix != "" {
		command = t.prefix + "\n" + command
	}

	extra := t.env
	if t.envFn != nil {
		extra = t.envFn()
	}
	cmd, err := useRunner(t.fs).Bash(runCtx, command, t.cwd, extra)
	if err != nil {
		return err.Error(), true
	}
	return runStreamed(runCtx, cmd, p.Timeout, "pigo-bash")
}
