// Package models is the provider catalog used by --list-models and /model.
package models

func init() {
	registerBuiltins()
	if err := applyEmbeddedCatalog(); err != nil {
		panic(err)
	}
}

func anthropicPromptCache() *PromptCache {
	return &PromptCache{Short: 300, Long: 3600}
}

func registerBuiltins() {
	// Model rows for providers covered by models.dev come from catalog.json.
	RegisterProvider(ProviderSpec{
		ID:         "anthropic",
		Env:        []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_API_KEY"},
		BaseURL:    "https://api.anthropic.com",
		DefaultAPI: "anthropic-messages",
		DefaultID:  "claude-sonnet-4-5",
	})
	RegisterProvider(ProviderSpec{
		ID:         "openai",
		Env:        []string{"OPENAI_API_KEY"},
		BaseURL:    "https://api.openai.com",
		DefaultAPI: "openai-responses",
		DefaultID:  "gpt-4o",
	})
	RegisterProvider(ProviderSpec{
		ID:         "opencode",
		Env:        []string{"OPENCODE_API_KEY"},
		BaseURL:    "https://opencode.ai/zen",
		DefaultAPI: "opencode",
		DefaultID:  "claude-sonnet-4",
	})
	RegisterProvider(ProviderSpec{
		ID:         "google",
		Env:        []string{"GEMINI_API_KEY"},
		BaseURL:    "https://generativelanguage.googleapis.com",
		DefaultAPI: "google-generative-ai",
		DefaultID:  "gemini-2.5-pro",
	})
	RegisterProvider(ProviderSpec{
		ID:         "amazon-bedrock",
		Env:        []string{"AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE", "AWS_ACCESS_KEY_ID"},
		DefaultAPI: "bedrock-converse-stream",
		DefaultID:  "anthropic.claude-sonnet-4-5-20250929-v1:0",
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
		DefaultAPI:    "pigo-messages",
		DefaultID:     "balanced",
		Models:        publicRadiusModels(),
		RefreshModels: refreshRadius,
	})
}
