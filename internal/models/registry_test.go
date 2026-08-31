package models

import (
	"testing"
)

func TestLookupUsesRegisteredAPI(t *testing.T) {
	t.Setenv("PIGO_TEST_ISOLATION", "1")
	RegisterProvider(ProviderSpec{
		ID:         "test-reg",
		DefaultAPI: "test-api",
		DefaultID:  "m1",
		Models: []Model{
			{Provider: "test-reg", ID: "m1", API: "custom-api"},
		},
	})
	m, ok := Lookup("test-reg", "m1")
	if !ok {
		t.Fatal("lookup missed registered model")
	}
	if m.API != "custom-api" {
		t.Fatalf("api = %q, want custom-api", m.API)
	}
	if APIFor("test-reg", "unknown") != "test-api" {
		t.Fatalf("unknown model should use default api, got %q", APIFor("test-reg", "unknown"))
	}
}

func TestAvailableFiltersByAuthenticated(t *testing.T) {
	got := Available([]string{"anthropic"})
	if len(got) == 0 {
		t.Fatal("expected anthropic models")
	}
	for _, m := range got {
		if m.Provider != "anthropic" {
			t.Fatalf("unauthenticated provider leaked: %+v", m)
		}
	}
}

func TestPickInitialSkipsUnauthenticatedDefault(t *testing.T) {
	got := PickInitial(PickOpts{
		SavedProvider: "anthropic",
		SavedModel:    "claude-sonnet-4",
		Authenticated: []string{"openai"},
	})
	if got.Provider != "openai" || got.ID == "" {
		t.Fatalf("pick = %+v, want openai default", got)
	}
}

func TestPickInitialHonorsCLI(t *testing.T) {
	got := PickInitial(PickOpts{
		CLIProvider:   "anthropic",
		CLIModel:      "claude-haiku-4",
		Authenticated: []string{"openai"},
	})
	if got.Provider != "anthropic" || got.ID != "claude-haiku-4" {
		t.Fatalf("cli pick = %+v", got)
	}
}

func TestBudgetTokensOverride(t *testing.T) {
	t.Cleanup(func() { SetThinkingBudgets(nil) })
	if BudgetTokens("high") != 10000 {
		t.Fatalf("default high = %d", BudgetTokens("high"))
	}
	SetThinkingBudgets(map[string]int{"high": 42})
	if BudgetTokens("high") != 42 {
		t.Fatalf("override high = %d", BudgetTokens("high"))
	}
	if BudgetTokens("off") != 0 {
		t.Fatal("off should be 0")
	}
}

func TestBuiltinCacheReadAndMaxTokens(t *testing.T) {
	if CacheReadPerToken("anthropic", "claude-sonnet-4") <= 0 {
		t.Fatal("builtin sonnet should have cache-read price")
	}
	if MaxTokens("anthropic", "claude-sonnet-4") != 64000 {
		t.Fatalf("maxTokens=%d", MaxTokens("anthropic", "claude-sonnet-4"))
	}
	if CacheReadPerToken("openai", "gpt-4o") <= 0 || MaxTokens("openai", "gpt-4o") != 16384 {
		t.Fatal("openai gpt-4o catalog")
	}
}

func TestOverlayMergesCostAndMaxTokens(t *testing.T) {
	ClearOverlays()
	t.Cleanup(ClearOverlays)
	SetUserOverlay("anthropic", []Model{{
		ID:        "claude-sonnet-4",
		Cost:      &Cost{CacheRead: 9.99},
		MaxTokens: 111,
	}})
	m, ok := Lookup("anthropic", "claude-sonnet-4")
	if !ok {
		t.Fatal("missing")
	}
	if m.Cost == nil || m.Cost.CacheRead != 9.99 {
		t.Fatalf("cost=%+v", m.Cost)
	}
	if m.MaxTokens != 111 {
		t.Fatalf("maxTokens=%d", m.MaxTokens)
	}
}
