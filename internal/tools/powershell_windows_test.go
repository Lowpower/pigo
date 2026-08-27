//go:build windows

package tools

import (
	"strings"
	"testing"
)

func TestPowershellStreamsAndRuns(t *testing.T) {
	out, isErr := run(t, powershellTool{}, map[string]any{"command": "Write-Output hello-ps"})
	if isErr || !strings.Contains(out, "hello-ps") {
		t.Fatalf("powershell = %q isErr=%v", out, isErr)
	}
}
