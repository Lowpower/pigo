//go:build windows

package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
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
	return "Execute a PowerShell command in the current working directory. Returns combined stdout and stderr. Optionally provide a timeout in seconds."
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

func extraPlatformTools() []Tool { return []Tool{powershellTool{}} }
