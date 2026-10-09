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
