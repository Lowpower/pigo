package models

import "testing"

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
