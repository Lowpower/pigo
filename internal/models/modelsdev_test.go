package models

import (
	"testing"
)

func TestModelsFromDevMapsLimitsCostInputAndEffort(t *testing.T) {
	raw := []byte(`{
		"claude-sonnet-4-5": {
			"id": "claude-sonnet-4-5",
			"name": "Claude Sonnet 4.5",
			"reasoning": true,
			"reasoning_options": [
				{"type": "budget_tokens", "min": 1024},
				{"type": "effort", "values": ["none", "low", "high"]}
			],
			"limit": {"context": 200000, "output": 64000},
			"cost": {"input": 3, "output": 15, "cache_read": 0.3, "cache_write": 3.75},
			"modalities": {"input": ["text", "image", "pdf"], "output": ["text"]}
		},
		"img-only": {
			"id": "img-only",
			"name": "Picture",
			"modalities": {"input": ["text"], "output": ["image"]}
		},
		"toggle-only": {
			"id": "toggle-only",
			"name": "Toggle",
			"reasoning": true,
			"reasoning_options": [{"type": "toggle"}],
			"limit": {"context": 10, "output": 5},
			"modalities": {"input": ["text"], "output": ["text"]}
		}
	}`)
	got, err := FromDev("anthropic", "anthropic-messages", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (image model dropped): %+v", len(got), got)
	}
	if got[0].ID != "claude-sonnet-4-5" || got[1].ID != "toggle-only" {
		t.Fatalf("ids = %s, %s", got[0].ID, got[1].ID)
	}
	sonnet := got[0]
	if sonnet.Provider != "anthropic" || sonnet.API != "anthropic-messages" {
		t.Fatalf("identity = %+v", sonnet)
	}
	if sonnet.Name != "Claude Sonnet 4.5" || sonnet.ContextWindow != 200000 || sonnet.MaxTokens != 64000 {
		t.Fatalf("limits = %+v", sonnet)
	}
	if sonnet.Cost == nil || sonnet.Cost.Input != 3 || sonnet.Cost.Output != 15 || sonnet.Cost.CacheRead != 0.3 || sonnet.Cost.CacheWrite != 3.75 {
		t.Fatalf("cost = %+v", sonnet.Cost)
	}
	if len(sonnet.Input) != 3 || sonnet.Input[0] != "text" || sonnet.Input[2] != "pdf" {
		t.Fatalf("input = %#v", sonnet.Input)
	}
	if sonnet.Reasoning == nil || !*sonnet.Reasoning {
		t.Fatalf("reasoning = %v", sonnet.Reasoning)
	}
	if sonnet.ThinkingLevelMap["off"] == nil || *sonnet.ThinkingLevelMap["off"] != "none" {
		t.Fatalf("off = %v", sonnet.ThinkingLevelMap["off"])
	}
	if sonnet.ThinkingLevelMap["low"] == nil || *sonnet.ThinkingLevelMap["low"] != "low" {
		t.Fatalf("low = %v", sonnet.ThinkingLevelMap["low"])
	}
	if sonnet.ThinkingLevelMap["high"] == nil || *sonnet.ThinkingLevelMap["high"] != "high" {
		t.Fatalf("high = %v", sonnet.ThinkingLevelMap["high"])
	}
	for _, level := range []string{"minimal", "medium", "xhigh", "max"} {
		if _, ok := sonnet.ThinkingLevelMap[level]; !ok || sonnet.ThinkingLevelMap[level] != nil {
			t.Fatalf("%s = %v present=%v", level, sonnet.ThinkingLevelMap[level], ok)
		}
	}
	toggle := got[1]
	if toggle.Reasoning == nil || !*toggle.Reasoning {
		t.Fatal("toggle should be reasoning")
	}
	if toggle.ThinkingLevelMap != nil {
		t.Fatalf("toggle map = %#v", toggle.ThinkingLevelMap)
	}
	if toggle.Cost != nil {
		t.Fatalf("missing cost = %+v", toggle.Cost)
	}
}

func TestModelsDevProviderID(t *testing.T) {
	id, ok := DevProviderID("together")
	if !ok || id != "togetherai" {
		t.Fatalf("together = %q ok=%v", id, ok)
	}
	id, ok = DevProviderID("anthropic")
	if !ok || id != "anthropic" {
		t.Fatalf("anthropic = %q ok=%v", id, ok)
	}
	if _, ok := DevProviderID("llama.cpp"); ok {
		t.Fatal("llama.cpp should stay local")
	}
	if _, ok := DevProviderID("radius"); ok {
		t.Fatal("radius should stay local")
	}
	if _, ok := DevProviderID("kimi-coding"); ok {
		t.Fatal("kimi-coding is not on models.dev")
	}
}
