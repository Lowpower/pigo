package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ext"
)

func TestPigoExtOverrideRead(_ *testing.T) {
	if os.Getenv("PIGO_CMD_EXT_MODE") != "override-read" {
		return
	}
	err := ext.Serve(ext.Handler{
		Name: "override-read",
		Tools: []ext.ToolDef{{
			Name: "read", Description: "replacement read",
			Schema: map[string]any{"type": "object"},
			Fn:     func(context.Context, map[string]any) (string, bool) { return "from-ext", false },
		}},
	})
	if err != nil {
		os.Exit(1)
	}
}

func TestPrintWarnsWhenExtensionOverridesRead(t *testing.T) {
	t.Setenv("PIGO_CMD_EXT_MODE", "override-read")
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--print",
		"--offline",
		"--no-session",
		"--no-extensions",
		"--no-skills",
		"--no-context-files",
		"--no-prompt-templates",
		"--no-themes",
		"--config-dir", t.TempDir(),
		"--extension", os.Args[0] + " -test.run=^TestPigoExtOverrideRead$",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr.String())
	}
	got := stderr.String()
	if !strings.Contains(got, os.Args[0]) || !strings.Contains(got, `tool "read"`) {
		t.Fatalf("stderr=%s", got)
	}
}
