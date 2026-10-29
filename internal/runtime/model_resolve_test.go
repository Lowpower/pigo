package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
)

func registerScopedProvider(t *testing.T, id, modelID, env, key string) {
	t.Helper()
	models.RegisterProvider(models.ProviderSpec{
		ID:         id,
		Env:        []string{env},
		DefaultAPI: "openai-completions",
		Models:     []models.Model{{ID: modelID}},
	})
	auth.RegisterProvider(auth.Provider{
		ID:     id,
		APIKey: &auth.APIKeyHandler{Env: []string{env}},
	})
	t.Setenv(env, key)
	t.Cleanup(func() {
		models.UnregisterProvider(id)
		auth.UnregisterProvider(id)
	})
}

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, id := range models.ProviderIDs() {
		spec, ok := models.LookupProvider(id)
		if !ok {
			continue
		}
		for _, name := range spec.Env {
			t.Setenv(name, "")
		}
	}
}

func TestNewScopedModelsKeepsDuplicateExactID(t *testing.T) {
	clearProviderEnv(t)
	registerScopedProvider(t, "zz-scope-a", "gpt-x", "PIGO_SCOPE_A", "sk-a")
	registerScopedProvider(t, "zz-scope-b", "gpt-x", "PIGO_SCOPE_B", "sk-b")
	e := newScopedEngine(t, Options{Models: []string{"gpt-x"}})
	defer e.Close()
	if !scopedHas(e, "zz-scope-a", "gpt-x") || !scopedHas(e, "zz-scope-b", "gpt-x") {
		t.Fatalf("scoped = %+v", e.Scoped)
	}
	warn := strings.Join(e.ModelWarnings(), "\n")
	if !strings.Contains(warn, `Pattern "gpt-x" matches several providers: zz-scope-a, zz-scope-b.`) {
		t.Fatalf("warnings = %q", warn)
	}
}

func TestNewEnabledModelsKeepsDuplicateExactID(t *testing.T) {
	clearProviderEnv(t)
	registerScopedProvider(t, "zz-scope-a", "gpt-x", "PIGO_SCOPE_A", "sk-a")
	registerScopedProvider(t, "zz-scope-b", "gpt-x", "PIGO_SCOPE_B", "sk-b")
	e := newScopedEngine(t, Options{UserConfig: &config.Config{EnabledModels: []string{"gpt-x"}}})
	defer e.Close()
	if !scopedHas(e, "zz-scope-a", "gpt-x") || !scopedHas(e, "zz-scope-b", "gpt-x") {
		t.Fatalf("scoped = %+v", e.Scoped)
	}
	if len(e.ModelWarnings()) != 1 {
		t.Fatalf("warnings = %q", e.ModelWarnings())
	}
}

func TestNewScopedModelsSkipUnauthenticatedDuplicate(t *testing.T) {
	clearProviderEnv(t)
	registerScopedProvider(t, "zz-scope-a", "gpt-x", "PIGO_SCOPE_A", "")
	registerScopedProvider(t, "zz-scope-b", "gpt-x", "PIGO_SCOPE_B", "sk-b")
	e := newScopedEngine(t, Options{Models: []string{"gpt-x"}})
	defer e.Close()
	if scopedHas(e, "zz-scope-a", "gpt-x") || !scopedHas(e, "zz-scope-b", "gpt-x") {
		t.Fatalf("scoped = %+v", e.Scoped)
	}
	if len(e.ModelWarnings()) != 0 {
		t.Fatalf("warnings = %q", e.ModelWarnings())
	}
}

func newScopedEngine(t *testing.T, opts Options) *Engine {
	t.Helper()
	opts.Cwd = t.TempDir()
	opts.AgentDir = t.TempDir()
	opts.Offline = true
	opts.NoTools = true
	opts.NoSkills = true
	opts.NoExtensions = true
	e, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func scopedHas(e *Engine, provider, id string) bool {
	for _, s := range e.Scoped {
		if s.Provider == provider && s.ID == id {
			return true
		}
	}
	return false
}
