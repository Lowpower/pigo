package models

import "testing"

func TestAnnotateBuiltinOpenAIMidConvoEffort(t *testing.T) {
	for _, id := range []string{"gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1-sol"} {
		got := annotateBuiltin(Model{Provider: "openai", API: "openai-responses", ID: id})
		if got.Compat == nil || !got.Compat.SupportsMidConvoEffort {
			t.Fatalf("%s compat = %+v", id, got.Compat)
		}
	}
	astra := annotateBuiltin(Model{Provider: "openai", API: "openai-responses", ID: "gpt-6-astra"})
	if !astra.Compat.SupportsExplicitPromptCacheMode {
		t.Fatal("gpt-6-astra lost explicit prompt cache mode")
	}
	kept := annotateBuiltin(Model{
		Provider: "openai", API: "openai-responses", ID: "gpt-6-astra",
		Compat: &Compat{ThinkingFormat: "keep"},
	})
	if kept.Compat.ThinkingFormat != "keep" || kept.Compat.SupportsExplicitPromptCacheMode || !kept.Compat.SupportsMidConvoEffort {
		t.Fatalf("existing compat = %+v", kept.Compat)
	}
	bareAPI := annotateBuiltin(Model{Provider: "openai", ID: "gpt-6-luna"})
	if bareAPI.Compat == nil || !bareAPI.Compat.SupportsMidConvoEffort {
		t.Fatalf("empty api compat = %+v", bareAPI.Compat)
	}

	for _, m := range []Model{
		{Provider: "openai", API: "openai-responses", ID: "gpt-5.4"},
		{Provider: "openai", API: "openai-completions", ID: "gpt-6-luna"},
		{Provider: "azure-openai-responses", API: "azure-openai-responses", ID: "gpt-6-luna"},
		{Provider: "openai-codex", API: "openai-codex-responses", ID: "gpt-6.1-sol"},
		{Provider: "openrouter", API: "openai-responses", ID: "openai/gpt-6-luna"},
	} {
		got := annotateBuiltin(m)
		if got.Compat != nil && got.Compat.SupportsMidConvoEffort {
			t.Fatalf("%s/%s compat = %+v", m.Provider, m.ID, got.Compat)
		}
	}
}

func TestCatalogOpenAIMidConvoEffort(t *testing.T) {
	for _, id := range []string{"gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1-sol"} {
		m, ok := Lookup("openai", id)
		if !ok || m.Compat == nil || !m.Compat.SupportsMidConvoEffort || m.API != "openai-responses" {
			t.Fatalf("%s ok=%v api=%q compat=%+v", id, ok, m.API, m.Compat)
		}
	}
	astra, ok := Lookup("openai", "gpt-6-astra")
	if !ok || astra.Compat == nil || !astra.Compat.SupportsExplicitPromptCacheMode {
		t.Fatalf("astra = %+v ok=%v", astra.Compat, ok)
	}
	if m, ok := Lookup("openai", "gpt-5.4"); ok && m.Compat != nil && m.Compat.SupportsMidConvoEffort {
		t.Fatalf("gpt-5.4 compat = %+v", m.Compat)
	}
	if m, ok := Lookup("azure-openai-responses", "gpt-6-luna"); ok && m.Compat != nil && m.Compat.SupportsMidConvoEffort {
		t.Fatalf("azure luna compat = %+v", m.Compat)
	}
}
