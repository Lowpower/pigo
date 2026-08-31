package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/Lowpower/pigo/internal/sandbox"
	"github.com/Lowpower/pigo/internal/shell"
)

// bashTool executes a shell command via shell.GetConfig (Git Bash / PATH / WSL).
type bashTool struct {
	prefix string
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

	runCtx := ctx
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(p.Timeout)*time.Second)
		defer cancel()
	}

	command := p.Command
	if t.prefix != "" {
		command = t.prefix + "\n" + command
	}

	cmd, err := bashCmd(runCtx, command, "")
	if err != nil {
		return err.Error(), true
	}
	return runStreamed(runCtx, cmd, p.Timeout, "pigo-bash")
}

func bashCmd(ctx context.Context, command, dir string) (*exec.Cmd, error) {
	cfg, err := shell.GetConfig()
	if err != nil {
		return nil, err
	}
	cwd := dir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	name, argv := sandbox.Command(command, cwd, "")
	var cmd *exec.Cmd
	if name == "bwrap" {
		cmd = exec.CommandContext(ctx, name, argv...)
		shell.PrepareContext(cmd)
	} else {
		cmd = shell.CommandContext(ctx, cfg, command)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd, nil
}
