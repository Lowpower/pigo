package models

import "testing"

func TestThinProvidersAreRegistered(t *testing.T) {
	want := map[string]string{
		"ant-ling":                   "openai-completions",
		"baseten":                    "openai-completions",
		"cerebras":                   "openai-completions",
		"deepseek":                   "openai-completions",
		"groq":                       "openai-completions",
		"huggingface":                "openai-completions",
		"kimi-coding":                "anthropic-messages",
		"minimax":                    "anthropic-messages",
		"minimax-cn":                 "anthropic-messages",
		"moonshotai":                 "openai-completions",
		"moonshotai-cn":              "openai-completions",
		"nvidia":                     "openai-completions",
		"openrouter":                 "openai-completions",
		"qwen-token-plan":            "openai-completions",
		"qwen-token-plan-cn":         "openai-completions",
		"qwen-token-plan-individual": "openai-completions",
		"together":                   "openai-completions",
		"vercel-ai-gateway":          "anthropic-messages",
		"xiaomi":                     "openai-completions",
		"xiaomi-token-plan-cn":       "openai-completions",
		"xiaomi-token-plan-ams":      "openai-completions",
		"xiaomi-token-plan-sgp":      "openai-completions",
		"zai":                        "openai-completions",
		"zai-coding-cn":              "openai-completions",
	}
	for id, api := range want {
		spec, ok := LookupProvider(id)
		if !ok {
			t.Errorf("missing provider %s", id)
			continue
		}
		if spec.DefaultAPI != api {
			t.Errorf("%s DefaultAPI = %q, want %q", id, spec.DefaultAPI, api)
		}
		if spec.DefaultID == "" || len(spec.Env) == 0 || spec.BaseURL == "" {
			t.Errorf("%s missing default/env/base: %+v", id, spec)
		}
		if APIFor(id, spec.DefaultID) != api {
			t.Errorf("%s APIFor(%s) = %q, want %q", id, spec.DefaultID, APIFor(id, spec.DefaultID), api)
		}
	}
}

func TestAvailableIncludesGroqWhenAuthenticated(t *testing.T) {
	got := Available([]string{"groq"})
	if len(got) == 0 {
		t.Fatal("expected groq models when authenticated")
	}
	for _, m := range got {
		if m.Provider != "groq" {
			t.Fatalf("unexpected provider %s", m.Provider)
		}
		if m.API != "openai-completions" {
			t.Fatalf("groq api = %q", m.API)
		}
	}
}

func TestAvailableOmitsThinProviderWhenUnauthenticated(t *testing.T) {
	for _, m := range Available([]string{"openai"}) {
		if m.Provider == "groq" || m.Provider == "minimax" {
			t.Fatalf("unauthenticated provider leaked: %+v", m)
		}
	}
}

func TestNvidiaDefaultHeaders(t *testing.T) {
	spec, ok := LookupProvider("nvidia")
	if !ok {
		t.Fatal("missing nvidia")
	}
	if spec.Headers["NVCF-POLL-SECONDS"] != "3600" {
		t.Fatalf("nvidia headers = %#v", spec.Headers)
	}
}

func TestMixedProvidersAreRegistered(t *testing.T) {
	want := map[string]string{
		"xai":                   "openai-responses",
		"fireworks":             "anthropic-messages",
		"github-copilot":        "openai-completions",
		"opencode-go":           "openai-completions",
		"cloudflare-workers-ai": "openai-completions",
		"cloudflare-ai-gateway": "anthropic-messages",
	}
	for id, api := range want {
		spec, ok := LookupProvider(id)
		if !ok {
			t.Errorf("missing provider %s", id)
			continue
		}
		if spec.DefaultAPI != api {
			t.Errorf("%s DefaultAPI = %q, want %q", id, spec.DefaultAPI, api)
		}
		if spec.DefaultID == "" || spec.BaseURL == "" {
			t.Errorf("%s missing default/base: %+v", id, spec)
		}
	}
	copilot, _ := LookupProvider("github-copilot")
	if copilot.Headers["Copilot-Integration-Id"] != "vscode-chat" {
		t.Fatalf("copilot headers = %#v", copilot.Headers)
	}
}

func TestAzureOpenAIResponsesRegistered(t *testing.T) {
	spec, ok := LookupProvider("azure-openai-responses")
	if !ok {
		t.Fatal("missing azure-openai-responses")
	}
	if spec.DefaultAPI != "azure-openai-responses" || spec.DefaultID == "" {
		t.Fatalf("spec = %+v", spec)
	}
	if APIFor("azure-openai-responses", spec.DefaultID) != "azure-openai-responses" {
		t.Fatalf("api = %q", APIFor("azure-openai-responses", spec.DefaultID))
	}
}
