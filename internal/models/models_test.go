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

func TestResolvePatternsInRestrictsList(t *testing.T) {
	list := []Model{
		{Provider: "anthropic", ID: "claude-sonnet-4"},
		{Provider: "openai", ID: "gpt-4o"},
	}
	got := ResolvePatternsIn([]string{"anthropic/*"}, list)
	if len(got) != 1 || got[0].Provider != "anthropic" {
		t.Fatalf("%+v", got)
	}
}

func TestResolvePatternsInPreservesPatternOrder(t *testing.T) {
	list := []Model{
		{Provider: "anthropic", ID: "claude-sonnet-4"},
		{Provider: "openai", ID: "gpt-4o"},
	}
	got := ResolvePatternsIn([]string{"openai/gpt-4o", "anthropic/claude-sonnet-4"}, list)
	if len(got) != 2 || got[0].ID != "gpt-4o" || got[1].ID != "claude-sonnet-4" {
		t.Fatalf("order %+v", got)
	}
}

func TestUnmatchedPatterns(t *testing.T) {
	list := []Model{{Provider: "anthropic", ID: "claude-sonnet-4"}}
	got := UnmatchedPatterns([]string{"anthropic/claude-sonnet-4", "missing/model", "nope/*"}, list)
	if len(got) != 2 || got[0] != "missing/model" || got[1] != "nope/*" {
		t.Fatalf("%v", got)
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
