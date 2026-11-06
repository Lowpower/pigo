package runtime

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
)

func TestRuntimeExtMode(_ *testing.T) {
	kind := runtimeExtArgKind()
	if kind == "" {
		return
	}
	err := ext.Serve(runtimeExtHandler(kind))
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func runtimeExtArgKind() string {
	for _, a := range os.Args {
		if strings.HasPrefix(a, "extmode=") {
			return strings.TrimPrefix(a, "extmode=")
		}
	}
	return ""
}

func runtimeExtHandler(kind string) ext.Handler {
	switch kind {
	case "override-read":
		return ext.Handler{
			Name: "override-read",
			Tools: []ext.ToolDef{{
				Name: "read", Description: "replacement read",
				Schema: map[string]any{"type": "object"},
				Fn:     func(context.Context, map[string]any) (string, bool) { return "from-first", false },
			}},
		}
	case "override-read-again":
		return ext.Handler{
			Name: "override-read-again",
			Tools: []ext.ToolDef{{
				Name: "read", Description: "second read",
				Schema: map[string]any{"type": "object"},
				Fn:     func(context.Context, map[string]any) (string, bool) { return "from-second", false },
			}},
		}
	case "marker":
		return ext.Handler{
			Name: "marker-ext",
			Tools: []ext.ToolDef{{
				Name: "marker_tool", Description: "still loaded",
				Schema: map[string]any{"type": "object"},
				Fn:     func(context.Context, map[string]any) (string, bool) { return "marker-ok", false },
			}},
			Commands: []ext.CommandDef{{
				Name: "ping", Description: "extension only", Fn: func(string) {},
			}},
		}
	case "nameless":
		return ext.Handler{
			Name: "nameless-ext",
			Commands: []ext.CommandDef{{
				Name: " ", Description: "blank", Fn: func(string) {},
			}},
		}
	case "no-handler":
		return ext.Handler{
			Name: "no-handler-ext",
			Commands: []ext.CommandDef{{
				Name: "review", Description: "missing handler",
			}},
		}
	case "builtin-help":
		return ext.Handler{
			Name: "help-ext",
			Commands: []ext.CommandDef{{
				Name: "help", Description: "extension help", Fn: func(string) {},
			}},
		}
	case "flag-verbose":
		return ext.Handler{
			Name: "flag-ext",
			Flags: []ext.FlagDef{
				{Name: "verbose", Type: "boolean", Description: "verbose"},
				{Name: "plan", Type: "boolean", Description: "plan"},
			},
		}
	default:
		return ext.Handler{Name: kind}
	}
}

func TestExtensionOverrideBuiltinReadWarnsOnce(t *testing.T) {
	e := newOverrideEngine(t, nil,
		extModeSpec("override-read"),
		extModeSpec("override-read-again"),
		extModeSpec("marker"),
	)
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if strings.Count(warns, `tool "read"`) != 1 || !strings.Contains(warns, os.Args[0]) {
		t.Fatalf("warnings=%q", warns)
	}
	if strings.Contains(warns, "marker_tool") || strings.Contains(warns, `command "ping"`) {
		t.Fatalf("warnings=%q", warns)
	}
	call, result, isErr, ok := e.Tools.Ready("read", map[string]any{})
	if !ok || isErr {
		t.Fatalf("ready=%q isErr=%v ok=%v", result, isErr, ok)
	}
	out, isErr := call.Execute(context.Background())
	if isErr || out != "from-second" {
		t.Fatalf("read=%q isErr=%v", out, isErr)
	}
	if !e.Tools.Has("marker_tool") {
		t.Fatal("marker extension missing")
	}
}

func TestNamelessCommandFailsOneExtension(t *testing.T) {
	e := newOverrideEngine(t, nil, extModeSpec("nameless"), extModeSpec("marker"))
	if len(e.Hosts) != 1 {
		t.Fatalf("hosts=%d", len(e.Hosts))
	}
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if !strings.Contains(warns, "non-empty string name") || !strings.Contains(warns, "nameless-ext") {
		t.Fatalf("warnings=%q", warns)
	}
	if !e.Tools.Has("marker_tool") {
		t.Fatal("marker extension missing")
	}
	for _, c := range e.SlashCommands() {
		if strings.TrimSpace(c.Name) == "" {
			t.Fatalf("empty command registered: %+v", e.SlashCommands())
		}
	}
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	if !e.Tools.Has("marker_tool") || len(e.Hosts) != 1 {
		t.Fatalf("reload hosts=%d hasMarker=%v", len(e.Hosts), e.Tools.Has("marker_tool"))
	}
	if warns := strings.Join(e.ExtensionWarnings(), "\n"); !strings.Contains(warns, "non-empty string name") {
		t.Fatalf("reload warnings=%q", warns)
	}
}

func TestCommandWithoutHandlerFailsExtension(t *testing.T) {
	e := newOverrideEngine(t, nil, extModeSpec("no-handler"), extModeSpec("marker"))
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if !strings.Contains(warns, "handler()") || !strings.Contains(warns, "review") || !strings.Contains(warns, "no-handler-ext") {
		t.Fatalf("warnings=%q", warns)
	}
	if !e.Tools.Has("marker_tool") || len(e.Hosts) != 1 {
		t.Fatalf("hosts=%d marker=%v", len(e.Hosts), e.Tools.Has("marker_tool"))
	}
}

func TestExtensionCommandOverrideWarnsAndRenames(t *testing.T) {
	e := newOverrideEngine(t, nil, extModeSpec("builtin-help"))
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if !strings.Contains(warns, os.Args[0]) || !strings.Contains(warns, `command "help"`) {
		t.Fatalf("warnings=%q", warns)
	}
	var found bool
	for _, c := range e.SlashCommands() {
		if c.Name == "help:2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands=%+v", e.SlashCommands())
	}
}

func TestExtensionFlagOverrideWarnsForKnownFlag(t *testing.T) {
	e := newOverrideEngine(t, []string{"verbose"}, extModeSpec("flag-verbose"))
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if !strings.Contains(warns, os.Args[0]) || !strings.Contains(warns, `flag "verbose"`) {
		t.Fatalf("warnings=%q", warns)
	}
	if strings.Contains(warns, `flag "plan"`) {
		t.Fatalf("warnings=%q", warns)
	}
}

func extModeSpec(kind string) string {
	return os.Args[0] + " -test.run=^TestRuntimeExtMode$ extmode=" + kind
}

func newOverrideEngine(t *testing.T, flags []string, specs ...string) *Engine {
	t.Helper()
	e, err := New(context.Background(), Options{
		Cwd:           t.TempDir(),
		AgentDir:      t.TempDir(),
		Offline:       true,
		NoSkills:      true,
		NoExtensions:  true,
		CLIExtensions: specs,
		BuiltinFlags:  flags,
		Config:        config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}
