package models

import "testing"

func TestEmbeddedCatalogPromptCostTiers(t *testing.T) {
	ClearOverlays()
	t.Cleanup(ClearOverlays)

	sol, ok := Lookup("amazon-bedrock", "global.openai.gpt-6-sol")
	if !ok || sol.Cost == nil || len(sol.Cost.Tiers) != 1 {
		t.Fatalf("gpt-6-sol cost = %+v ok=%v", sol.Cost, ok)
	}
	tier := sol.Cost.Tiers[0]
	if tier.InputTokensAbove != 272000 || tier.Input != 4 || tier.Output != 15 || tier.CacheRead != 0.4 || tier.CacheWrite != 5 {
		t.Fatalf("gpt-6-sol tier = %+v", tier)
	}

	flat, ok := Lookup("amazon-bedrock", "openai.gpt-5.4")
	if !ok || flat.Cost == nil || len(flat.Cost.Tiers) != 0 {
		t.Fatalf("bedrock gpt-5.4 should stay untiered: %+v ok=%v", flat.Cost, ok)
	}

	haiku, ok := Lookup("opencode", "claude-haiku-5-5")
	if !ok || haiku.Cost == nil || len(haiku.Cost.Tiers) != 1 {
		t.Fatalf("opencode haiku cost = %+v ok=%v", haiku.Cost, ok)
	}
	h := haiku.Cost.Tiers[0]
	if h.InputTokensAbove != 100000 || h.Input != 0.5 || h.Output != 2.5 || h.CacheRead != 0.05 || h.CacheWrite != 0.625 {
		t.Fatalf("opencode haiku tier = %+v", h)
	}

	gemini, ok := Lookup("google", "gemini-3.1-pro-preview")
	if !ok || gemini.Cost == nil || len(gemini.Cost.Tiers) != 1 {
		t.Fatalf("gemini cost = %+v ok=%v", gemini.Cost, ok)
	}
	g := gemini.Cost.Tiers[0]
	if g.InputTokensAbove != 200000 || g.Input != 4 || g.Output != 18 || g.CacheRead != 0.4 || g.CacheWrite != 0 {
		t.Fatalf("gemini tier = %+v", g)
	}

	gateway, ok := Lookup("vercel-ai-gateway", "openai/gpt-5.4")
	if !ok || gateway.Cost == nil || len(gateway.Cost.Tiers) != 1 || gateway.Cost.Tiers[0].InputTokensAbove != 271999 {
		t.Fatalf("vercel gpt-5.4 tier = %+v ok=%v", gateway.Cost, ok)
	}
}

func TestEmbeddedCatalogDefaults(t *testing.T) {
	ClearOverlays()
	t.Cleanup(ClearOverlays)

	file, err := loadCatalogFile()
	if err != nil {
		t.Fatal(err)
	}
	if file.License != "MIT" || file.Copyright == "" || file.Source == "" || file.SHA256 == "" {
		t.Fatalf("catalog header = %+v", file)
	}
	if len(file.Providers) == 0 {
		t.Fatal("embedded catalog has no providers")
	}
	var sawEffort bool
	for id, list := range file.Providers {
		if len(list) == 0 {
			t.Fatalf("%s has no models", id)
		}
		if id == "cloudflare-ai-gateway" {
			list = AppendGatewayWorkersAI(list, file.Providers["cloudflare-workers-ai"])
		}
		spec, ok := LookupProvider(id)
		if !ok {
			t.Fatalf("embedded provider %s is not registered", id)
		}
		if err := CatalogDefaultOK(id, spec.DefaultID, list); err != nil {
			t.Fatal(err)
		}
		for _, m := range list {
			if len(m.ThinkingLevelMap) > 0 {
				sawEffort = true
			}
			if m.ContextWindow < 0 || m.MaxTokens < 0 {
				t.Fatalf("%s/%s negative limits", id, m.ID)
			}
		}
	}
	if !sawEffort {
		t.Fatal("embedded catalog has no thinkingLevelMap")
	}
	for _, id := range []string{"llama.cpp", "radius", "ant-ling", "kimi-coding", "qwen-token-plan-individual", "openai-codex"} {
		if _, ok := file.Providers[id]; ok {
			t.Fatalf("%s should stay out of the models.dev snapshot", id)
		}
	}
	together, ok := LookupProvider("together")
	if !ok || len(together.Models) < 2 {
		t.Fatalf("together models = %d", len(together.Models))
	}
}

func TestGatewayMirrorsWorkersAIDefault(t *testing.T) {
	const id = "workers-ai/@cf/moonshotai/kimi-k2.6"
	m, ok := Lookup("cloudflare-ai-gateway", id)
	if !ok || m.API != "openai-completions" || m.ContextWindow <= 0 || m.MaxTokens <= 0 {
		t.Fatalf("gateway default = %+v ok=%v", m, ok)
	}
	spec, _ := LookupProvider("cloudflare-ai-gateway")
	if spec.DefaultID != id || spec.DefaultAPI != "anthropic-messages" {
		t.Fatalf("gateway spec = %+v", spec)
	}
	worker, ok := Lookup("cloudflare-workers-ai", "@cf/moonshotai/kimi-k2.6")
	if !ok || worker.Provider != "cloudflare-workers-ai" || worker.ID != "@cf/moonshotai/kimi-k2.6" {
		t.Fatalf("workers model = %+v ok=%v", worker, ok)
	}
}

func TestAppendGatewayWorkersAISkipsExisting(t *testing.T) {
	gateway := []Model{{ID: "workers-ai/already", API: "openai-completions"}}
	workers := []Model{{ID: "already", Name: "Old"}, {ID: "new", Name: "New", Input: []string{"text"}}}
	got := AppendGatewayWorkersAI(gateway, workers)
	if len(got) != 2 {
		t.Fatalf("len = %d, %+v", len(got), got)
	}
	if got[0].ID != "workers-ai/already" || got[0].Name != "" {
		t.Fatalf("existing row = %+v", got[0])
	}
	if got[1].ID != "workers-ai/new" || got[1].Name != "New" || got[1].API != "openai-completions" || got[1].Provider != "cloudflare-ai-gateway" {
		t.Fatalf("mirrored row = %+v", got[1])
	}
	if len(workers[1].Input) != 1 || workers[1].Provider != "" {
		t.Fatalf("source mutated: %+v", workers[1])
	}
}
