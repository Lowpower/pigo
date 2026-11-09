// Package models is the provider catalog used by --list-models and /model.
package models

func init() {
	registerBuiltins()
	if err := applyEmbeddedCatalog(); err != nil {
		panic(err)
	}
	appendOpenAIClassifier()
	appendBuiltinClassifiers()
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

func appendBuiltinClassifiers() {
	RegisterProvider(ProviderSpec{
		ID:         "typesafe",
		Name:       "TypeSafe API key",
		Env:        []string{"TYPESAFE_API_KEY"},
		BaseURL:    "https://api.typesafe.ai/v1",
		DefaultAPI: "typesafe-system-one",
	})
	appendProviderModels("typesafe", classifierModel("typesafe", "jev-latest", "Jev", "typesafe-system-one", "https://api.typesafe.ai/v1", 64000, nil))
	appendProviderModels("openrouter",
		classifierModel("openrouter", "typesafe/jev-1.13", "Jev", "typesafe-system-one", "https://openrouter.ai/api/v1", 32000, nil),
		classifierModel("openrouter", "~typesafe/jev-latest", "Jev", "typesafe-system-one", "https://openrouter.ai/api/v1", 32000, nil),
	)
	cfBase := "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai"
	appendProviderModels("cloudflare-workers-ai",
		classifierModel("cloudflare-workers-ai", "typesafe/jev", "Jev", "cloudflare-workers-ai-system-one", cfBase, 64000, nil),
		classifierModel("cloudflare-workers-ai", "@cf/cloudflare/clef", "Clef", "cloudflare-workers-ai-system-one", cfBase, 64000, &Cost{Input: 0.24}),
		classifierModel("cloudflare-workers-ai", "@cf/cloudflare/clef-flash", "Clef Flash", "cloudflare-workers-ai-system-one", cfBase, 64000, &Cost{Input: 0.09}),
	)
	appendProviderModels("vercel-ai-gateway", classifierModel("vercel-ai-gateway", "typesafe-ai/jev", "Jev", "typesafe-system-one", "https://ai-gateway.vercel.sh/typesafe/v1", 32000, nil))
	appendProviderModels("opencode",
		classifierModel("opencode", "jev-1.13", "Jev", "typesafe-system-one", "https://opencode.ai/inference/v1", 32000, nil),
		classifierModel("opencode", "jev-1.13-free", "Jev", "typesafe-system-one", "https://opencode.ai/inference/v1", 32000, nil),
	)
}

func classifierModel(provider, id, name, api, base string, window int, cost *Cost) Model {
	return Model{
		Provider: provider, ID: id, Name: name, Type: "classifier", API: api,
		BaseURL: base, Input: []string{"text"}, ContextWindow: window, Cost: cost,
	}
}

func appendProviderModels(provider string, extra ...Model) {
	spec, ok := LookupProvider(provider)
	if !ok {
		return
	}
	spec.Models = append(spec.Models, extra...)
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
