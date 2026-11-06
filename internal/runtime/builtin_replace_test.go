package runtime

import (
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/config"
)

func TestUserToolReplacesCodemodeBuiltin(t *testing.T) {
	e := newBuiltinEngine(t, Options{
		CLIExtensions: []string{extModeSpec("replace-codemode")},
		Config:        config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	ids := strings.Join(e.BuiltinExtensionIDs(), " ")
	if strings.Contains(ids, "builtin:codemode") {
		t.Fatalf("codemode still loaded: %s", ids)
	}
	if !strings.Contains(ids, "builtin:mcp") || !strings.Contains(ids, "builtin:llama.cpp") || !strings.Contains(ids, "builtin:tool-search") {
		t.Fatalf("ids = %s", ids)
	}
	if src := extensionSources(e.LoadedExtensions()); strings.Contains(src, "builtin:codemode") {
		t.Fatalf("bug sources = %s", src)
	}
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if !strings.Contains(warns, os.Args[0]) || !strings.Contains(warns, "tool `codemode`") || !strings.Contains(warns, "built-in extension `codemode` was not loaded") {
		t.Fatalf("warnings=%q", warns)
	}
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	if ids := strings.Join(e.BuiltinExtensionIDs(), " "); strings.Contains(ids, "builtin:codemode") {
		t.Fatalf("reload still loaded codemode: %s", ids)
	}
}

func TestUserCommandReplacesMCPBuiltin(t *testing.T) {
	e := newBuiltinEngine(t, Options{
		CLIExtensions: []string{extModeSpec("replace-mcp")},
		Config:        config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	ids := strings.Join(e.BuiltinExtensionIDs(), " ")
	if strings.Contains(ids, "builtin:mcp") {
		t.Fatalf("mcp still loaded: %s", ids)
	}
	if !strings.Contains(ids, "builtin:codemode") {
		t.Fatalf("ids = %s", ids)
	}
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if !strings.Contains(warns, "command `/mcp`") || !strings.Contains(warns, "built-in extension `mcp` was not loaded") {
		t.Fatalf("warnings=%q", warns)
	}
}

func TestUserToolReplacesToolSearchBuiltin(t *testing.T) {
	e := newBuiltinEngine(t, Options{
		CLIExtensions: []string{extModeSpec("replace-tool-search")},
		Config:        config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	ids := strings.Join(e.BuiltinExtensionIDs(), " ")
	if strings.Contains(ids, "builtin:tool-search") {
		t.Fatalf("tool-search still loaded: %s", ids)
	}
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if !strings.Contains(warns, "tool `tool_search`") || !strings.Contains(warns, "built-in extension `tool-search` was not loaded") {
		t.Fatalf("warnings=%q", warns)
	}
}

func TestLlamaBuiltinStaysWhenCommandMatches(t *testing.T) {
	e := newBuiltinEngine(t, Options{
		CLIExtensions: []string{extModeSpec("replace-llama")},
		Config:        config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	ids := strings.Join(e.BuiltinExtensionIDs(), " ")
	if !strings.Contains(ids, "builtin:llama.cpp") {
		t.Fatalf("llama.cpp was omitted: %s", ids)
	}
	warns := strings.Join(e.ExtensionWarnings(), "\n")
	if strings.Contains(warns, "built-in extension `llama.cpp` was not loaded") {
		t.Fatalf("warnings=%q", warns)
	}
}
