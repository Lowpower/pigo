// Package models is the provider catalog used by --list-models and /model.
package models

func init() {
	registerBuiltins()
}

func usd(input, output, cacheRead, cacheWrite float64) *Cost {
	return &Cost{Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite}
}

func registerBuiltins() {
	anthropicCost := map[string]*Cost{
		"claude-sonnet-4": usd(3, 15, 0.30, 3.75),
		"claude-opus-4":   usd(15, 75, 1.50, 18.75),
		"claude-haiku-4":  usd(1, 5, 0.10, 1.25),
	}
	openaiCost := map[string]*Cost{
		"gpt-4o":  usd(2.50, 10, 1.25, 2.50),
		"gpt-4.1": usd(2, 8, 0.50, 2),
	}
	googleCost := map[string]*Cost{
		"gemini-2.5-pro":   usd(1.25, 10, 0.315, 1.25),
		"gemini-2.5-flash": usd(0.15, 0.60, 0.0375, 0.15),
	}

	RegisterProvider(ProviderSpec{
		ID:         "anthropic",
		Env:        []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
		BaseURL:    "https://api.anthropic.com",
		DefaultAPI: "anthropic-messages",
		DefaultID:  "claude-sonnet-4",
		Models: []Model{
			{Provider: "anthropic", ID: "claude-sonnet-4", API: "anthropic-messages", Cost: anthropicCost["claude-sonnet-4"], MaxTokens: 64000},
			{Provider: "anthropic", ID: "claude-opus-4", API: "anthropic-messages", Cost: anthropicCost["claude-opus-4"], MaxTokens: 32000},
			{Provider: "anthropic", ID: "claude-haiku-4", API: "anthropic-messages", Cost: anthropicCost["claude-haiku-4"], MaxTokens: 64000},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "openai",
		Env:        []string{"OPENAI_API_KEY"},
		BaseURL:    "https://api.openai.com",
		DefaultAPI: "openai-responses",
		DefaultID:  "gpt-4o",
		Models: []Model{
			{Provider: "openai", ID: "gpt-4o", API: "openai-responses", Cost: openaiCost["gpt-4o"], MaxTokens: 16384},
			{Provider: "openai", ID: "gpt-4.1", API: "openai-responses", Cost: openaiCost["gpt-4.1"], MaxTokens: 32768},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "opencode",
		Env:        []string{"OPENCODE_API_KEY"},
		BaseURL:    "https://opencode.ai/zen",
		DefaultAPI: "opencode",
		DefaultID:  "claude-sonnet-4",
		Models: []Model{
			{Provider: "opencode", ID: "claude-sonnet-4", API: "opencode", Cost: anthropicCost["claude-sonnet-4"], MaxTokens: 64000},
			{Provider: "opencode", ID: "gpt-4o", API: "opencode", Cost: openaiCost["gpt-4o"], MaxTokens: 16384},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "google",
		Env:        []string{"GEMINI_API_KEY"},
		BaseURL:    "https://generativelanguage.googleapis.com",
		DefaultAPI: "google-generative-ai",
		DefaultID:  "gemini-2.5-pro",
		Models: []Model{
			{Provider: "google", ID: "gemini-2.5-pro", API: "google-generative-ai", Cost: googleCost["gemini-2.5-pro"], MaxTokens: 65536},
			{Provider: "google", ID: "gemini-2.5-flash", API: "google-generative-ai", Cost: googleCost["gemini-2.5-flash"], MaxTokens: 65536},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "amazon-bedrock",
		Env:        []string{"AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE", "AWS_ACCESS_KEY_ID"},
		DefaultAPI: "bedrock-converse-stream",
		DefaultID:  "anthropic.claude-sonnet-4-v1:0",
		Models: []Model{
			{Provider: "amazon-bedrock", ID: "anthropic.claude-sonnet-4-v1:0", API: "bedrock-converse-stream", Cost: anthropicCost["claude-sonnet-4"], MaxTokens: 64000},
			{Provider: "amazon-bedrock", ID: "anthropic.claude-haiku-4-v1:0", API: "bedrock-converse-stream", Cost: anthropicCost["claude-haiku-4"], MaxTokens: 64000},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "llama.cpp",
		Env:        []string{"LLAMA_BASE_URL", "LLAMA_API_KEY"},
		BaseURL:    "http://127.0.0.1:8080/v1",
		DefaultAPI: "openai-completions",
		RefreshModels: func(store CatalogStore) error {
			return refreshLlama(store)
		},
	})
	registerExtraProviders()
	RegisterProvider(ProviderSpec{
		ID:            "radius",
		Name:          "Radius API key",
		Env:           []string{"RADIUS_API_KEY"},
		DefaultAPI:    "pi-messages",
		RefreshModels: refreshRadius,
	})
}
