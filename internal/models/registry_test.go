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

func TestAnthropicPromptCacheLifetime(t *testing.T) {
	m, ok := Lookup("anthropic", "claude-sonnet-4")
	if !ok || m.PromptCache == nil || m.PromptCache.Short != 300 || m.PromptCache.Long != 3600 {
		t.Fatalf("promptCache=%+v", m.PromptCache)
	}
	openai, ok := Lookup("openai", "gpt-4o")
	if !ok || openai.PromptCache != nil {
		t.Fatalf("openai promptCache=%+v", openai.PromptCache)
	}
}

func TestOverlayMergesPromptCache(t *testing.T) {
	ClearOverlays()
	t.Cleanup(ClearOverlays)
	SetUserOverlay("openai", []Model{{
		ID:          "gpt-4o",
		PromptCache: &PromptCache{Short: 120},
	}})
	m, ok := Lookup("openai", "gpt-4o")
	if !ok || m.PromptCache == nil || m.PromptCache.Short != 120 {
		t.Fatalf("promptCache=%+v", m.PromptCache)
	}
}

func TestOverlayMergesFallbackAndStrict(t *testing.T) {
	on := true
	RegisterProvider(ProviderSpec{
		ID: "compat-merge", DefaultAPI: "anthropic-messages", DefaultID: "m",
		Models: []Model{{
			Provider: "compat-merge", ID: "m",
			Compat: &Compat{ThinkingFormat: "zai"},
		}},
	})
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("compat-merge")
	})
	SetUserOverlay("compat-merge", []Model{{
		ID: "m",
		Compat: &Compat{
			SupportsStrictMode:    &on,
			AllowedFallbackModels: []FallbackModel{},
		},
	}})
	m, ok := Lookup("compat-merge", "m")
	if !ok || m.Compat == nil || m.Compat.SupportsStrictMode == nil || !*m.Compat.SupportsStrictMode {
		t.Fatalf("strict = %+v ok=%v", m.Compat, ok)
	}
	if m.Compat.AllowedFallbackModels == nil || len(m.Compat.AllowedFallbackModels) != 0 {
		t.Fatalf("fallbacks = %#v", m.Compat.AllowedFallbackModels)
	}
	if m.Compat.ThinkingFormat != "zai" {
		t.Fatalf("thinking format lost: %+v", m.Compat)
	}
}

func TestOverlayMergesImageResizeOntoExistingModel(t *testing.T) {
	height := 800
	quality := 70
	width := 1568
	on := true
	RegisterProvider(ProviderSpec{
		ID: "resize-merge", DefaultAPI: "openai-completions", DefaultID: "vision",
		Models: []Model{{
			Provider: "resize-merge", ID: "vision",
			Compat: &Compat{ThinkingFormat: "zai"},
			InputLimits: &InputLimits{Images: &ImageInputLimits{Resize: &ImageResize{
				MaxHeight:   &height,
				JPEGQuality: &quality,
			}}},
		}},
	})
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("resize-merge")
	})
	SetUserOverlay("resize-merge", []Model{
		{
			ID: "vision",
			Compat: &Compat{
				SupportsStrictMode: &on,
			},
			InputLimits: &InputLimits{Images: &ImageInputLimits{Resize: &ImageResize{
				MaxWidth: &width,
			}}},
		},
		{
			ID: "fresh",
			InputLimits: &InputLimits{Images: &ImageInputLimits{Resize: &ImageResize{
				MaxWidth: &width,
				MaxBytes: &height,
			}}},
		},
	})

	m, ok := Lookup("resize-merge", "vision")
	if !ok || m.InputLimits == nil || m.InputLimits.Images == nil || m.InputLimits.Images.Resize == nil {
		t.Fatalf("resize profile missing: %+v ok=%v", m.InputLimits, ok)
	}
	got := m.InputLimits.Images.Resize
	if got.MaxWidth == nil || *got.MaxWidth != 1568 {
		t.Fatalf("maxWidth = %v", got.MaxWidth)
	}
	if got.MaxHeight == nil || *got.MaxHeight != 800 {
		t.Fatalf("maxHeight = %v", got.MaxHeight)
	}
	if got.JPEGQuality == nil || *got.JPEGQuality != 70 {
		t.Fatalf("jpegQuality = %v", got.JPEGQuality)
	}
	if got.MaxBytes != nil {
		t.Fatalf("maxBytes = %v, want unset", got.MaxBytes)
	}
	if m.Compat == nil || m.Compat.ThinkingFormat != "zai" {
		t.Fatalf("compat thinking format lost: %+v", m.Compat)
	}
	if m.Compat.SupportsStrictMode == nil || !*m.Compat.SupportsStrictMode {
		t.Fatalf("strict = %+v", m.Compat)
	}

	fresh, ok := Lookup("resize-merge", "fresh")
	if !ok || fresh.InputLimits == nil || fresh.InputLimits.Images == nil || fresh.InputLimits.Images.Resize == nil {
		t.Fatalf("new model profile = %+v ok=%v", fresh.InputLimits, ok)
	}
	if fresh.InputLimits.Images.Resize.MaxWidth == nil || *fresh.InputLimits.Images.Resize.MaxWidth != 1568 {
		t.Fatalf("fresh maxWidth = %v", fresh.InputLimits.Images.Resize.MaxWidth)
	}
	if fresh.InputLimits.Images.Resize.MaxBytes == nil || *fresh.InputLimits.Images.Resize.MaxBytes != 800 {
		t.Fatalf("fresh maxBytes = %v", fresh.InputLimits.Images.Resize.MaxBytes)
	}
}

func TestOverlayWithoutImageResizeKeepsExistingProfile(t *testing.T) {
	width := 1024
	RegisterProvider(ProviderSpec{
		ID: "resize-keep", DefaultAPI: "openai-completions", DefaultID: "vision",
		Models: []Model{{
			Provider: "resize-keep", ID: "vision",
			Compat: &Compat{ThinkingFormat: "openai"},
			InputLimits: &InputLimits{Images: &ImageInputLimits{Resize: &ImageResize{
				MaxWidth: &width,
			}}},
		}},
	})
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("resize-keep")
	})
	SetUserOverlay("resize-keep", []Model{{
		ID:        "vision",
		MaxTokens: 1234,
	}})
	m, ok := Lookup("resize-keep", "vision")
	if !ok || m.MaxTokens != 1234 {
		t.Fatalf("maxTokens = %d ok=%v", m.MaxTokens, ok)
	}
	if m.InputLimits == nil || m.InputLimits.Images == nil || m.InputLimits.Images.Resize == nil ||
		m.InputLimits.Images.Resize.MaxWidth == nil || *m.InputLimits.Images.Resize.MaxWidth != 1024 {
		t.Fatalf("profile cleared: %+v", m.InputLimits)
	}
	if m.Compat == nil || m.Compat.ThinkingFormat != "openai" {
		t.Fatalf("compat = %+v", m.Compat)
	}
}

func TestOverlayMergesSamplingParams(t *testing.T) {
	RegisterProvider(ProviderSpec{
		ID: "samp-merge", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []Model{{
			Provider: "samp-merge", ID: "m",
			SamplingParams: map[string]any{
				"temperature": 1,
				"top_p":       0.95,
				"top_k":       0,
			},
		}},
	})
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("samp-merge")
	})
	SetRemoteOverlay("samp-merge", []Model{{
		ID: "m",
		SamplingParams: map[string]any{
			"top_p":              0.8,
			"repetition_penalty": 1.1,
		},
	}})
	SetUserOverlay("samp-merge", []Model{
		{
			ID: "m",
			SamplingParams: map[string]any{
				"top_p": 0.9,
			},
		},
		{
			ID: "fresh",
			SamplingParams: map[string]any{
				"top_k":       0,
				"min_p":       nil,
				"dry_allowed": false,
			},
		},
		{
			ID:             "empty-only",
			SamplingParams: map[string]any{},
		},
	})

	m, ok := Lookup("samp-merge", "m")
	if !ok {
		t.Fatal("missing m")
	}
	if m.SamplingParams["temperature"] != 1 {
		t.Fatalf("temperature = %#v", m.SamplingParams["temperature"])
	}
	if m.SamplingParams["top_p"] != 0.9 {
		t.Fatalf("top_p = %#v, want user override 0.9", m.SamplingParams["top_p"])
	}
	if m.SamplingParams["top_k"] != 0 {
		t.Fatalf("top_k = %#v, want 0", m.SamplingParams["top_k"])
	}
	if m.SamplingParams["repetition_penalty"] != 1.1 {
		t.Fatalf("repetition_penalty = %#v", m.SamplingParams["repetition_penalty"])
	}

	fresh, ok := Lookup("samp-merge", "fresh")
	if !ok || fresh.SamplingParams["top_k"] != 0 || fresh.SamplingParams["dry_allowed"] != false {
		t.Fatalf("fresh = %#v ok=%v", fresh.SamplingParams, ok)
	}
	if _, present := fresh.SamplingParams["min_p"]; !present {
		t.Fatal("null min_p was dropped")
	}
	empty, ok := Lookup("samp-merge", "empty-only")
	if !ok {
		t.Fatal("empty-only model missing")
	}
	if len(empty.SamplingParams) != 0 {
		t.Fatalf("empty samplingParams = %#v", empty.SamplingParams)
	}

	SetUserOverlay("samp-merge", []Model{{
		ID:             "m",
		SamplingParams: map[string]any{},
	}})
	kept, ok := Lookup("samp-merge", "m")
	if !ok || kept.SamplingParams["temperature"] != 1 || kept.SamplingParams["top_p"] != 0.8 || kept.SamplingParams["top_k"] != 0 {
		t.Fatalf("empty object cleared params: %#v ok=%v", kept.SamplingParams, ok)
	}
}
