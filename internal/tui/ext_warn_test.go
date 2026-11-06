package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/slash"
)

func TestTUIExtOverrideRead(_ *testing.T) {
	if os.Getenv("PIGO_TUI_EXT_MODE") != "override-read" {
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

func TestStartupShowsExtensionOverrideWarning(t *testing.T) {
	t.Setenv("PIGO_TUI_EXT_MODE", "override-read")
	eng, err := runtime.New(context.Background(), runtime.Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		Offline:      true,
		NoSkills:     true,
		NoExtensions: true,
		CLIExtensions: []string{
			os.Args[0] + " -test.run=^TestTUIExtOverrideRead$",
		},
		Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })

	m := New(testCfg())
	m.engine = eng
	view := strings.ReplaceAll(m.View(), "\n", "")
	if !strings.Contains(view, os.Args[0]) || !strings.Contains(view, `tool "read"`) {
		t.Fatalf("startup missing override warning:\n%s", m.View())
	}

	on := true
	m.cfg.QuietStartupFlag = &on
	quiet := strings.ReplaceAll(m.View(), "\n", "")
	if !strings.Contains(quiet, `tool "read"`) || !strings.Contains(quiet, os.Args[0]) {
		t.Fatalf("quiet startup hid override warning:\n%s", m.View())
	}

	next, _ := m.handleSlash(slash.Command{Name: "reload"})
	reloaded := next.(Model)
	var note string
	for _, ent := range reloaded.transcript {
		note += ent.rendered
	}
	if !strings.Contains(note, `tool "read"`) || !strings.Contains(note, os.Args[0]) {
		t.Fatalf("reload note = %q", note)
	}
}
