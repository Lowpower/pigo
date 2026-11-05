package models

import (
	"reflect"
	"testing"
)

func TestPi130XiaomiThinkingLevels(t *testing.T) {
	want := []string{"off", "high"}
	for _, provider := range []string{"xiaomi", "xiaomi-token-plan-cn", "xiaomi-token-plan-ams", "xiaomi-token-plan-sgp"} {
		spec, ok := LookupProvider(provider)
		if !ok || len(spec.Models) == 0 {
			t.Fatalf("%s has no models", provider)
		}
		var reasoning int
		for _, m := range spec.Models {
			if m.Reasoning == nil || !*m.Reasoning {
				continue
			}
			reasoning++
			if !reflect.DeepEqual(m.ThinkingLevelsFor(), want) {
				t.Fatalf("%s/%s levels = %v", provider, m.ID, m.ThinkingLevelsFor())
			}
			if m.Compat == nil || m.Compat.ThinkingFormat != "deepseek" || m.Compat.SupportsReasoningEffort {
				t.Fatalf("%s/%s compat = %+v", provider, m.ID, m.Compat)
			}
			if m.Compat.RequiresReasoningContentOnAssistantMessages == nil || !*m.Compat.RequiresReasoningContentOnAssistantMessages {
				t.Fatalf("%s/%s missing reasoning_content replay", provider, m.ID)
			}
		}
		if reasoning == 0 {
			t.Fatalf("%s has no reasoning models", provider)
		}
	}
}

func TestPi130CodexGPT61SolContext(t *testing.T) {
	m, ok := Lookup("openai-codex", "gpt-6.1-sol")
	if !ok {
		t.Fatal("missing openai-codex/gpt-6.1-sol")
	}
	if m.ContextWindow != 500000 || m.MaxTokens != 128000 {
		t.Fatalf("context=%d max=%d", m.ContextWindow, m.MaxTokens)
	}
}

func TestPi130ClaudeOffThinking(t *testing.T) {
	withOff := []string{"off", "low", "medium", "high", "xhigh", "max"}
	withoutOff := []string{"low", "medium", "high", "xhigh", "max"}
	for _, id := range []string{"claude-haiku-5-5", "claude-opus-5"} {
		m, ok := Lookup("anthropic", id)
		if !ok {
			t.Fatalf("missing anthropic/%s", id)
		}
		if !reflect.DeepEqual(m.ThinkingLevelsFor(), withOff) {
			t.Fatalf("%s levels = %v", id, m.ThinkingLevelsFor())
		}
	}
	opus55, ok := Lookup("anthropic", "claude-opus-5-5")
	if !ok || !reflect.DeepEqual(opus55.ThinkingLevelsFor(), withoutOff) {
		t.Fatalf("opus 5.5 levels = %v ok=%v", opus55.ThinkingLevelsFor(), ok)
	}
	bedrock, ok := Lookup("amazon-bedrock", "us.anthropic.claude-haiku-5-5")
	if !ok {
		t.Fatal("missing bedrock haiku")
	}
	for _, level := range bedrock.ThinkingLevelsFor() {
		if level == "off" {
			t.Fatal("bedrock haiku should keep off unsupported")
		}
	}
}

func TestPi130CloudflareDashedClaudeIDs(t *testing.T) {
	if _, ok := Lookup("cloudflare-ai-gateway", "anthropic/claude-opus-5-5"); !ok {
		t.Fatal("missing dashed anthropic/claude-opus-5-5")
	}
	if _, ok := Lookup("cloudflare-ai-gateway", "anthropic/claude-opus-5.5"); ok {
		t.Fatal("dotted anthropic/claude-opus-5.5 still registered")
	}
	if _, ok := Lookup("cloudflare-ai-gateway", "anthropic/claude-sonnet-4-5"); !ok {
		t.Fatal("missing dashed anthropic/claude-sonnet-4-5")
	}
	if _, ok := Lookup("cloudflare-ai-gateway", "openai/gpt-6.1-sol"); !ok {
		t.Fatal("openai gateway id was rewritten")
	}
	if _, ok := Lookup("cloudflare-ai-gateway", "workers-ai/@cf/moonshotai/kimi-k2.6"); !ok {
		t.Fatal("workers-ai mirror id changed")
	}
}

func TestPi130OpenCodeEndpointsAndMiniMax(t *testing.T) {
	zen, ok := LookupProvider("opencode")
	if !ok || zen.BaseURL != "https://opencode.ai/inference" {
		t.Fatalf("opencode base = %q", zen.BaseURL)
	}
	gozen, ok := LookupProvider("opencode-go")
	if !ok || gozen.BaseURL != "https://opencode.ai/inference/go" {
		t.Fatalf("opencode-go base = %q", gozen.BaseURL)
	}
	m27, ok := Lookup("opencode-go", "minimax-m2.7")
	if !ok || m27.API != "anthropic-messages" {
		t.Fatalf("minimax-m2.7 api = %q ok=%v", m27.API, ok)
	}
	m3, ok := Lookup("opencode-go", "minimax-m3")
	if !ok || m3.API != "openai-completions" {
		t.Fatalf("minimax-m3 api = %q ok=%v", m3.API, ok)
	}
}
