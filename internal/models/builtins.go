// Package models is the provider catalog used by --list-models and /model.
package models

func init() {
	registerBuiltins()
}

func registerBuiltins() {
	RegisterProvider(ProviderSpec{
		ID:         "anthropic",
		Env:        []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
		BaseURL:    "https://api.anthropic.com",
		DefaultAPI: "anthropic-messages",
		DefaultID:  "claude-sonnet-4",
		Models: []Model{
			{Provider: "anthropic", ID: "claude-sonnet-4", API: "anthropic-messages"},
			{Provider: "anthropic", ID: "claude-opus-4", API: "anthropic-messages"},
			{Provider: "anthropic", ID: "claude-haiku-4", API: "anthropic-messages"},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "openai",
		Env:        []string{"OPENAI_API_KEY"},
		BaseURL:    "https://api.openai.com",
		DefaultAPI: "openai-responses",
		DefaultID:  "gpt-4o",
		Models: []Model{
			{Provider: "openai", ID: "gpt-4o", API: "openai-responses"},
			{Provider: "openai", ID: "gpt-4.1", API: "openai-responses"},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "opencode",
		Env:        []string{"OPENCODE_API_KEY"},
		BaseURL:    "https://opencode.ai/zen",
		DefaultAPI: "opencode",
		DefaultID:  "claude-sonnet-4",
		Models: []Model{
			{Provider: "opencode", ID: "claude-sonnet-4", API: "opencode"},
			{Provider: "opencode", ID: "gpt-4o", API: "opencode"},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "google",
		Env:        []string{"GEMINI_API_KEY"},
		BaseURL:    "https://generativelanguage.googleapis.com",
		DefaultAPI: "google-generative-ai",
		DefaultID:  "gemini-2.5-pro",
		Models: []Model{
			{Provider: "google", ID: "gemini-2.5-pro", API: "google-generative-ai"},
			{Provider: "google", ID: "gemini-2.5-flash", API: "google-generative-ai"},
		},
	})
	RegisterProvider(ProviderSpec{
		ID:         "amazon-bedrock",
		Env:        []string{"AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE", "AWS_ACCESS_KEY_ID"},
		DefaultAPI: "bedrock-converse-stream",
		DefaultID:  "anthropic.claude-sonnet-4-v1:0",
		Models: []Model{
			{Provider: "amazon-bedrock", ID: "anthropic.claude-sonnet-4-v1:0", API: "bedrock-converse-stream"},
			{Provider: "amazon-bedrock", ID: "anthropic.claude-haiku-4-v1:0", API: "bedrock-converse-stream"},
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
