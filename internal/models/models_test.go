package models

import "testing"

func TestParseSpec(t *testing.T) {
	p, id, th := ParseSpec("openai/gpt-4o:high")
	if p != "openai" || id != "gpt-4o" || th != "high" {
		t.Fatalf("%s %s %s", p, id, th)
	}
	p, id, th = ParseSpec("claude-sonnet-4")
	if p != "" || id != "claude-sonnet-4" || th != "" {
		t.Fatalf("%s %s %s", p, id, th)
	}
}

func TestCycleAndThinking(t *testing.T) {
	if NextThinkingLevel("off") != "minimal" {
		t.Fatalf("next off = %s", NextThinkingLevel("off"))
	}
	if NextThinkingLevel("max") != "off" {
		t.Fatalf("wrap = %s", NextThinkingLevel("max"))
	}
	scoped := ResolvePatterns([]string{"anthropic/*"})
	if len(scoped) < 2 {
		t.Fatalf("scoped = %+v", scoped)
	}
	next, ok := Cycle("anthropic", "claude-sonnet-4", scoped, false)
	if !ok || next.Provider != "anthropic" {
		t.Fatalf("cycle = %+v ok=%v", next, ok)
	}
}
