package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestToolModifiersAdjustDefault(t *testing.T) {
	e := newToolEngine(t, Options{
		NoExtensions: true,
		ToolAllow:    []string{"+codemode", "-write"},
	})
	if strings.Join(enabledNames(e), ",") != "read,edit,bash" {
		t.Fatalf("enabled=%v", enabledNames(e))
	}
	if !strings.Contains(strings.Join(toolNames(e), ","), "write") {
		t.Fatal("write should stay registered")
	}
}

func TestToolGlobAndExclude(t *testing.T) {
	e := newToolEngine(t, Options{
		NoExtensions: true,
		ToolAllow:    []string{"ba*", "grep"},
	})
	if strings.Join(enabledNames(e), ",") != "bash,grep" {
		t.Fatalf("glob enabled=%v", enabledNames(e))
	}

	excluded := newToolEngine(t, Options{
		NoExtensions: true,
		ToolDeny:     []string{"ba*", "write"},
	})
	if strings.Join(enabledNames(excluded), ",") != "read,edit" {
		t.Fatalf("exclude enabled=%v", enabledNames(excluded))
	}
}

func TestInvalidToolListRejected(t *testing.T) {
	ctx := context.Background()
	_, err := New(ctx, Options{Cwd: t.TempDir(), AgentDir: t.TempDir(), ToolAllow: []string{"read", "+grep"}})
	if err == nil {
		t.Fatal("mixed --tools should fail")
	}
	_, err = New(ctx, Options{Cwd: t.TempDir(), AgentDir: t.TempDir(), ToolAllow: []string{"-ba*"}})
	if err == nil {
		t.Fatal("pattern in +name/-name should fail")
	}
}

func TestReloadEnablesNewToolsAndKeepsSessionChoices(t *testing.T) {
	agent := t.TempDir()
	e := newToolEngine(t, Options{AgentDir: agent, NoExtensions: true})
	e.SetActiveTools([]string{"read", "edit", "bash"})
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(`{"defaultTools":["+grep"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(enabledNames(e), ",") != "read,edit,bash,grep" {
		t.Fatalf("enabled=%v", enabledNames(e))
	}
}

func TestReloadKeepsToolsRemovedFromSettings(t *testing.T) {
	agent := t.TempDir()
	list := []string{"read", "bash", "edit", "write", "grep"}
	e := newToolEngine(t, Options{
		AgentDir:     agent,
		NoExtensions: true,
		Config:       config.Config{DefaultTools: &list},
	})
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(`{"defaultTools":["read","bash","edit","write"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(enabledNames(e), ",") != "read,write,edit,bash,grep" {
		t.Fatalf("enabled=%v", enabledNames(e))
	}
}

func TestExplicitToolsSkipSessionRestore(t *testing.T) {
	e := newToolEngine(t, Options{NoExtensions: true, ToolAllow: []string{"read"}})
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendActiveToolsChange([]string{"grep", "find"}); err != nil {
		t.Fatal(err)
	}
	e.AdoptSession(sess)
	if strings.Join(enabledNames(e), ",") != "read" {
		t.Fatalf("enabled=%v", enabledNames(e))
	}
}

func TestResumeAppliesToolModifiers(t *testing.T) {
	e := newToolEngine(t, Options{NoExtensions: true, ToolAllow: []string{"+grep", "-write"}})
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendActiveToolsChange([]string{"write", "read"}); err != nil {
		t.Fatal(err)
	}
	e.AdoptSession(sess)
	if strings.Join(enabledNames(e), ",") != "read,grep" {
		t.Fatalf("enabled=%v", enabledNames(e))
	}
}

func TestExplicitReloadRebuildsSelection(t *testing.T) {
	e := newToolEngine(t, Options{NoExtensions: true, ToolAllow: []string{"read"}, AgentDir: t.TempDir()})
	e.SetActiveTools([]string{"read", "grep"})
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(enabledNames(e), ",") != "read" {
		t.Fatalf("enabled=%v", enabledNames(e))
	}
}

func TestGlobMatchesExtensionNames(t *testing.T) {
	base := tools.NewBuiltins(tools.Options{})
	e := &Engine{Tools: tools.NewRegistry(append(base.List(),
		fnTool{name: "mcp__radius__a", schema: objectSchema()},
		fnTool{name: "mcp__radius__b", schema: objectSchema()},
		fnTool{name: "other", schema: objectSchema()},
	)...), Opts: Options{ToolAllow: []string{"mcp__radius__*"}}}
	e.installActiveTools(e.selectedActiveNames())
	if strings.Join(enabledNames(e), ",") != "mcp__radius__a,mcp__radius__b" {
		t.Fatalf("enabled=%v", enabledNames(e))
	}
}

func TestDefaultSelectionKeepsExtensionTools(t *testing.T) {
	base := tools.NewBuiltins(tools.Options{})
	e := &Engine{Tools: tools.NewRegistry(append(base.List(),
		fnTool{name: "custom", schema: objectSchema()},
	)...)}
	e.installActiveTools(e.selectedActiveNames())
	if strings.Join(enabledNames(e), ",") != "read,write,edit,bash,custom" {
		t.Fatalf("enabled=%v", enabledNames(e))
	}
}

func newToolEngine(t *testing.T, opts Options) *Engine {
	t.Helper()
	if opts.Cwd == "" {
		opts.Cwd = t.TempDir()
	}
	if opts.AgentDir == "" {
		opts.AgentDir = t.TempDir()
	}
	e, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}
