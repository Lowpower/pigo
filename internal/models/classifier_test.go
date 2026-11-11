package models

import "testing"

func TestBuiltinJevAndClefStayOutOfChatCatalog(t *testing.T) {
	want := []struct{ provider, id, api string }{
		{"typesafe", "jev-latest", "typesafe-system-one"},
		{"openrouter", "typesafe/jev-1.13", "typesafe-system-one"},
		{"openrouter", "~typesafe/jev-latest", "typesafe-system-one"},
		{"cloudflare-workers-ai", "typesafe/jev", "cloudflare-workers-ai-system-one"},
		{"cloudflare-workers-ai", "@cf/cloudflare/clef", "cloudflare-workers-ai-system-one"},
		{"cloudflare-workers-ai", "@cf/cloudflare/clef-flash", "cloudflare-workers-ai-system-one"},
		{"vercel-ai-gateway", "typesafe-ai/jev", "typesafe-system-one"},
		{"opencode", "jev-1.13", "typesafe-system-one"},
		{"opencode", "jev-1.13-free", "typesafe-system-one"},
	}
	for _, m := range Catalog() {
		for _, item := range want {
			if m.Provider == item.provider && m.ID == item.id {
				t.Fatalf("chat catalog includes %s/%s", item.provider, item.id)
			}
		}
	}
	for _, item := range want {
		got, ok := ModelOfType("classifier", item.provider, item.id)
		if !ok || got.API != item.api {
			t.Fatalf("%s/%s = %+v ok=%v", item.provider, item.id, got, ok)
		}
	}
	clef, _ := ModelOfType("classifier", "cloudflare-workers-ai", "@cf/cloudflare/clef")
	flash, _ := ModelOfType("classifier", "cloudflare-workers-ai", "@cf/cloudflare/clef-flash")
	if clef.Cost == nil || clef.Cost.Input != 0.24 || flash.Cost == nil || flash.Cost.Input != 0.09 {
		t.Fatalf("clef prices = %+v %+v", clef.Cost, flash.Cost)
	}
}

func TestBuiltinClassifierStaysOutOfChatCatalog(t *testing.T) {
	for _, m := range Catalog() {
		if m.Provider == "openai" && m.ID == "gpt-6-luna" && m.ModelType() != "chat" {
			t.Fatalf("chat catalog includes classifier %+v", m)
		}
	}
	chat, ok := Lookup("openai", "gpt-6-luna")
	if !ok || chat.ModelType() != "chat" {
		t.Fatalf("chat luna = %+v ok=%v", chat, ok)
	}
	got, ok := ModelOfType("classifier", "openai", "gpt-6-luna")
	if !ok || got.API != "openai-decisions" || got.ModelType() != "classifier" {
		t.Fatalf("classifier = %+v ok=%v", got, ok)
	}
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("base = %q", got.BaseURL)
	}
}

func TestOverlayClassifierDoesNotReplaceChatModel(t *testing.T) {
	t.Cleanup(ClearOverlays)
	SetRemoteOverlay("openai", []Model{{
		ID:   "gpt-6-luna",
		Type: "classifier",
		API:  "openai-decisions",
		Cost: &Cost{Input: 0.1},
	}})
	chat, ok := Lookup("openai", "gpt-6-luna")
	if !ok || chat.ModelType() != "chat" || chat.API == "openai-decisions" {
		t.Fatalf("chat luna = %+v ok=%v", chat, ok)
	}
	got, ok := ModelOfType("classifier", "openai", "gpt-6-luna")
	if !ok || got.Cost == nil || got.Cost.Input != 0.1 {
		t.Fatalf("classifier = %+v ok=%v", got, ok)
	}
}
