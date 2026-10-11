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
		"meta":                       "openai-responses",
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

func TestAvailableFiltersThinProviderByAuth(t *testing.T) {
	for _, m := range Available([]string{"openai"}) {
		if m.Provider == "groq" || m.Provider == "minimax" {
			t.Fatalf("unauthenticated provider leaked: %+v", m)
		}
	}
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
	if copilot.FilterModels == nil {
		t.Fatal("github-copilot missing FilterModels")
	}
	xai, _ := LookupProvider("xai")
	if xai.DefaultID != "grok-4.7" || DefaultID("xai") != "grok-4.7" {
		t.Fatalf("xai DefaultID = %q func = %q", xai.DefaultID, DefaultID("xai"))
	}
	offline, ok := Lookup("xai", "grok-4.7")
	if !ok || offline.API != "openai-responses" {
		t.Fatalf("xai offline model = %+v ok=%v", offline, ok)
	}
}

// offlineDefaultIDs is the offline fallback table.
// It matches pi defaultModelPerProvider except cloudflare-ai-gateway:
// pi uses workers-ai/@cf/moonshotai/kimi-k2.6, which is absent from the
// embedded models.dev snapshot, so the in-snapshot id stays.
func TestOfflineDefaultIDs(t *testing.T) {
	want := map[string]string{
		"amazon-bedrock":             "us.anthropic.claude-opus-4-6-v1",
		"anthropic":                  "claude-opus-4-8",
		"ant-ling":                   "Ring-2.6-1T",
		"azure-openai-responses":     "gpt-5.4",
		"baseten":                    "zai-org/GLM-5.2",
		"cerebras":                   "gpt-oss-120b",
		"cloudflare-ai-gateway":      "anthropic/claude-fable-5",
		"cloudflare-workers-ai":      "@cf/moonshotai/kimi-k2.6",
		"deepseek":                   "deepseek-v4-pro",
		"fireworks":                  "accounts/fireworks/models/kimi-k3",
		"github-copilot":             "gpt-5.4",
		"google":                     "gemini-3.1-pro-preview",
		"google-vertex":              "gemini-3.1-pro-preview",
		"groq":                       "openai/gpt-oss-120b",
		"huggingface":                "moonshotai/Kimi-K2.6",
		"kimi-coding":                "kimi-for-coding",
		"llama.cpp":                  "",
		"meta":                       "muse-spark-1.3",
		"minimax":                    "MiniMax-M2.7",
		"minimax-cn":                 "MiniMax-M2.7",
		"mistral":                    "devstral-medium-latest",
		"moonshotai":                 "kimi-k2.6",
		"moonshotai-cn":              "kimi-k2.6",
		"nvidia":                     "nvidia/nemotron-3-ultra-550b-a55b",
		"openai":                     "gpt-5.5",
		"openai-codex":               "gpt-6.1-sol",
		"opencode":                   "kimi-k2.6",
		"opencode-go":                "kimi-k3",
		"openrouter":                 "moonshotai/kimi-k2.6",
		"qwen-token-plan":            "qwen3.7-max",
		"qwen-token-plan-cn":         "qwen3.7-max",
		"qwen-token-plan-individual": "qwen3.8-max",
		"radius":                     "balanced",
		"together":                   "moonshotai/Kimi-K3",
		"vercel-ai-gateway":          "zai/glm-5.1",
		"xai":                        "grok-4.7",
		"xiaomi":                     "mimo-v2.5-pro",
		"xiaomi-token-plan-ams":      "mimo-v2.5-pro",
		"xiaomi-token-plan-cn":       "mimo-v2.5-pro",
		"xiaomi-token-plan-sgp":      "mimo-v2.5-pro",
		"zai":                        "glm-5.3",
		"zai-coding-cn":              "glm-5.3",
	}
	seen := map[string]bool{}
	for _, id := range ProviderIDs() {
		seen[id] = true
		spec, ok := LookupProvider(id)
		if !ok {
			t.Errorf("missing provider %s", id)
			continue
		}
		expected, listed := want[id]
		if !listed {
			t.Errorf("provider %s DefaultID %q is not in the offline default table", id, spec.DefaultID)
			continue
		}
		if spec.DefaultID != expected || DefaultID(id) != expected {
			t.Errorf("%s DefaultID = %q func = %q, want %q", id, spec.DefaultID, DefaultID(id), expected)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("offline default table lists unknown provider %s", id)
		}
	}
}

func TestExplicitModelBeatsOfflineDefault(t *testing.T) {
	ClearOverlays()
	t.Cleanup(ClearOverlays)
	cases := []struct {
		provider, def, user, remote string
	}{
		{provider: "xai", def: "grok-4.7", user: "grok-user", remote: "grok-remote"},
		{provider: "openai", def: "gpt-5.5", user: "gpt-user", remote: "gpt-remote"},
	}
	for _, tc := range cases {
		SetRemoteOverlay(tc.provider, []Model{{ID: tc.remote, API: "openai-responses"}})
		SetUserOverlay(tc.provider, []Model{{ID: tc.user, API: "openai-responses"}})
		if DefaultID(tc.provider) != tc.def {
			t.Fatalf("%s DefaultID = %q", tc.provider, DefaultID(tc.provider))
		}
		cli := PickInitial(PickOpts{CLIProvider: tc.provider, CLIModel: tc.user})
		if cli.Provider != tc.provider || cli.ID != tc.user {
			t.Fatalf("%s cli = %+v", tc.provider, cli)
		}
		saved := PickInitial(PickOpts{
			SavedProvider: tc.provider,
			SavedModel:    tc.remote,
			Authenticated: []string{tc.provider},
		})
		if saved.Provider != tc.provider || saved.ID != tc.remote {
			t.Fatalf("%s saved = %+v", tc.provider, saved)
		}
	}
}

func TestFilterByAvailableIDs(t *testing.T) {
	const provider = "filter-test"
	t.Cleanup(func() { ClearAvailableModelIDs(provider) })
	ClearAvailableModelIDs(provider)
	list := []Model{{ID: "keep"}, {ID: "drop"}}
	f := filterByAvailableIDs(provider)
	got := f(list)
	if len(got) != 2 {
		t.Fatalf("unfiltered = %v", got)
	}
	SetAvailableModelIDs(provider, []string{"keep"})
	got = f(list)
	if len(got) != 1 || got[0].ID != "keep" {
		t.Fatalf("filtered = %v", got)
	}
	SetAvailableModelIDs(provider, []string{"missing"})
	got = f(list)
	if len(got) != 0 {
		t.Fatalf("empty allowlist = %v", got)
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

func TestRemainingNewAPIProvidersRegistered(t *testing.T) {
	want := []struct {
		id, api, defaultID string
		needBase           bool
	}{
		{"google-vertex", "google-vertex", "gemini-3.1-pro-preview", false},
		{"mistral", "mistral-conversations", "devstral-medium-latest", true},
		{"openai-codex", "openai-codex-responses", "gpt-6.1-sol", true},
		{"radius", "pigo-messages", "balanced", false},
	}
	for _, tc := range want {
		spec, ok := LookupProvider(tc.id)
		if !ok {
			t.Errorf("missing provider %s", tc.id)
			continue
		}
		if spec.DefaultAPI != tc.api {
			t.Errorf("%s DefaultAPI = %q, want %q", tc.id, spec.DefaultAPI, tc.api)
		}
		if tc.defaultID != "" {
			if spec.DefaultID != tc.defaultID {
				t.Errorf("%s DefaultID = %q, want %q", tc.id, spec.DefaultID, tc.defaultID)
			}
			if APIFor(tc.id, spec.DefaultID) != tc.api {
				t.Errorf("%s APIFor(%s) = %q, want %q", tc.id, spec.DefaultID, APIFor(tc.id, spec.DefaultID), tc.api)
			}
		}
		if tc.needBase && spec.BaseURL == "" {
			t.Errorf("%s missing BaseURL", tc.id)
		}
	}
	if spec, _ := LookupProvider("radius"); spec.RefreshModels == nil {
		t.Error("radius missing RefreshModels")
	}
}
