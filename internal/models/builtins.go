// Package models is the provider catalog used by --list-models and /model.
package models

func init() {
	registerBuiltins()
	if err := applyEmbeddedCatalog(); err != nil {
		panic(err)
	}
	appendOpenAIClassifier()
}

// appendOpenAIClassifier adds the Decisions model next to the chat GPT-6 Luna row.
// pi v1.3.0 prices it at $0.10 / million input, doubling after 272K tokens.
func appendOpenAIClassifier() {
	spec, ok := LookupProvider("openai")
	if !ok {
		return
	}
	spec.Models = append(spec.Models, Model{
		Provider:      "openai",
		ID:            "gpt-6-luna",
		Name:          "GPT-6 Luna",
		Type:          "classifier",
		API:           "openai-decisions",
		BaseURL:       "https://api.openai.com/v1",
		Input:         []string{"text", "image"},
		ContextWindow: 922000,
		Cost: &Cost{
			Input: 0.1,
			Tiers: []CostTier{{InputTokensAbove: 272000, Input: 0.2}},
		},
	})
	RegisterProvider(spec)
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
		DefaultID:  "claude-opus-4-8",
	})
	RegisterProvider(ProviderSpec{
		ID:         "openai",
		Env:        []string{"OPENAI_API_KEY"},
		BaseURL:    "https://api.openai.com",
		DefaultAPI: "openai-responses",
		DefaultID:  "gpt-5.5",
	})
	RegisterProvider(ProviderSpec{
		ID:         "opencode",
		Env:        []string{"OPENCODE_API_KEY"},
		BaseURL:    "https://opencode.ai/inference",
		DefaultAPI: "opencode",
		DefaultID:  "kimi-k2.6",
	})
	RegisterProvider(ProviderSpec{
		ID:         "google",
		Env:        []string{"GEMINI_API_KEY"},
		BaseURL:    "https://generativelanguage.googleapis.com",
		DefaultAPI: "google-generative-ai",
		DefaultID:  "gemini-3.1-pro-preview",
	})
	RegisterProvider(ProviderSpec{
		ID:         "amazon-bedrock",
		Env:        []string{"AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE", "AWS_ACCESS_KEY_ID"},
		DefaultAPI: "bedrock-converse-stream",
		DefaultID:  "us.anthropic.claude-opus-4-6-v1",
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
