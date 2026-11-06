package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Lowpower/pigo/internal/bugreport"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/llama"
	"github.com/Lowpower/pigo/internal/models"
)

func TestBuiltinExtensionsDefaultSet(t *testing.T) {
	e := newBuiltinEngine(t, Options{})
	got := strings.Join(e.BuiltinExtensionIDs(), " ")
	want := "builtin:llama.cpp builtin:codemode builtin:tool-search builtin:mcp"
	if got != want {
		t.Fatalf("ids = %q", got)
	}
	if src := extensionSources(e.LoadedExtensions()); !strings.Contains(src, "builtin:llama.cpp") || !strings.Contains(src, "builtin:mcp") {
		t.Fatalf("bug sources = %s", src)
	}
	if e.disableLlama {
		t.Fatal("llama.cpp should stay available")
	}
}

func TestBuiltinExtensionsSettingsDisableMCP(t *testing.T) {
	agent := t.TempDir()
	writeSettings(t, agent, `{"extensions":["-builtin:mcp"]}`)
	e := newBuiltinEngine(t, Options{AgentDir: agent})
	got := strings.Join(e.BuiltinExtensionIDs(), " ")
	if strings.Contains(got, "builtin:mcp") {
		t.Fatalf("mcp still loaded: %s", got)
	}
	if !strings.Contains(got, "builtin:llama.cpp") || !strings.Contains(got, "builtin:tool-search") {
		t.Fatalf("ids = %s", got)
	}
}

func TestBuiltinExtensionsNoExtensionsReenableOne(t *testing.T) {
	e := newBuiltinEngine(t, Options{
		NoExtensions:  true,
		CLIExtensions: []string{"builtin:tool-search"},
	})
	if got := strings.Join(e.BuiltinExtensionIDs(), " "); got != "builtin:tool-search" {
		t.Fatalf("ids = %q", got)
	}
	if !e.disableLlama {
		t.Fatal("llama.cpp should be off")
	}
	for _, m := range e.VisibleModels() {
		if m.Provider == llama.ProviderID {
			t.Fatal("session catalog still lists llama.cpp")
		}
	}
	if _, ok := models.LookupProvider(llama.ProviderID); !ok {
		t.Fatal("static llama.cpp provider was unregistered")
	}
}

func TestBuiltinExtensionsNoExtensionsLoadsNone(t *testing.T) {
	e := newBuiltinEngine(t, Options{NoExtensions: true})
	if ids := e.BuiltinExtensionIDs(); len(ids) != 0 {
		t.Fatalf("ids = %v", ids)
	}
}

func TestBuiltinExtensionsNoToolsLoadsNone(t *testing.T) {
	e := newBuiltinEngine(t, Options{NoTools: true, CLIExtensions: []string{"builtin:mcp"}})
	if ids := e.BuiltinExtensionIDs(); len(ids) != 0 {
		t.Fatalf("ids = %v", ids)
	}
}

func TestBuiltinExtensionsUnknownName(t *testing.T) {
	_, err := New(context.Background(), Options{
		Cwd:           t.TempDir(),
		AgentDir:      t.TempDir(),
		Offline:       true,
		NoExtensions:  true,
		CLIExtensions: []string{"builtin:nope"},
		Config:        config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	if err == nil || err.Error() != "unknown built-in extension: builtin:nope" {
		t.Fatalf("err=%v", err)
	}
}

func TestBuiltinExtensionsCLIRestoresDisabled(t *testing.T) {
	agent := t.TempDir()
	writeSettings(t, agent, `{"extensions":["-builtin:tool-search"]}`)
	e := newBuiltinEngine(t, Options{
		AgentDir:      agent,
		CLIExtensions: []string{"builtin:tool-search"},
	})
	if !strings.Contains(strings.Join(e.BuiltinExtensionIDs(), " "), "builtin:tool-search") {
		t.Fatalf("ids = %v", e.BuiltinExtensionIDs())
	}
}

func TestDisabledLlamaSkipsCatalogRefresh(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("LLAMA_BASE_URL", srv.URL)

	e := newBuiltinEngine(t, Options{NoExtensions: true})
	if hits.Load() != 0 {
		t.Fatalf("disabled llama.cpp refreshed %d times", hits.Load())
	}
	if !e.disableLlama {
		t.Fatal("llama still enabled")
	}

	hits.Store(0)
	on := newBuiltinEngine(t, Options{})
	if hits.Load() == 0 {
		t.Fatal("enabled llama.cpp did not refresh")
	}
	if on.disableLlama {
		t.Fatal("default session disabled llama.cpp")
	}
}

func TestSavedLlamaFallsBackWhenBuiltinOff(t *testing.T) {
	agent := t.TempDir()
	writeSettings(t, agent, `{"extensions":["-builtin:llama.cpp"]}`)
	e := newBuiltinEngine(t, Options{
		AgentDir: agent,
		Config:   config.Config{Provider: "llama.cpp", Model: "local"},
	})
	if e.Provider == llama.ProviderID {
		t.Fatalf("provider = %s", e.Provider)
	}
	warns := strings.Join(e.ModelWarnings(), "\n")
	if !strings.Contains(warns, "builtin:llama.cpp is not loaded") {
		t.Fatalf("warnings = %q", warns)
	}
}

func newBuiltinEngine(t *testing.T, opts Options) *Engine {
	t.Helper()
	if opts.Cwd == "" {
		opts.Cwd = t.TempDir()
	}
	if opts.AgentDir == "" {
		opts.AgentDir = t.TempDir()
	}
	opts.Offline = true
	opts.NoSkills = true
	if opts.Config.Provider == "" {
		opts.Config.Provider = "anthropic"
		opts.Config.Model = "claude-sonnet-4"
	}
	e, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func extensionSources(exts []bugreport.Extension) string {
	var parts []string
	for _, ext := range exts {
		parts = append(parts, ext.Source)
	}
	return strings.Join(parts, " ")
}
