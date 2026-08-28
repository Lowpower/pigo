//go:build windows

package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/Lowpower/pigo/internal/shell"
)

const powershellUTF8 = "try { [Console]::OutputEncoding=[System.Text.Encoding]::UTF8 } catch {}\n"

type powershellTool struct{}

type powershellParams struct {
	Command string `json:"command" jsonschema:"description=PowerShell command to execute"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds (optional, no default timeout)"`
}

func (powershellTool) Name() string { return "powershell" }

func (powershellTool) Description() string {
	return fmt.Sprintf("Execute a PowerShell command in the current working directory. Returns stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.", DefaultMaxLines, DefaultMaxBytes/1024)
}

func (powershellTool) Schema() map[string]any { return schemaFor(&powershellParams{}) }

func (powershellTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	var p powershellParams
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
	cfg, err := shell.GetPowerShellConfig()
	if err != nil {
		return err.Error(), true
	}
	cmd := shell.CommandContext(runCtx, cfg, powershellUTF8+p.Command)
	return runStreamed(runCtx, cmd, p.Timeout, "pigo-powershell")
}

func extraPlatformTools() []Tool { return []Tool{powershellTool{}} }
