package models

import "testing"

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
