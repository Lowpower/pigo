package models

import "testing"

func boolPtr(v bool) *bool { return &v }

func strPtr(v string) *string { return &v }

func TestThinkingLevelsNonReasoning(t *testing.T) {
	m := Model{Reasoning: boolPtr(false)}
	if m.SupportsReasoning() {
		t.Fatal("expected no reasoning")
	}
	got := m.ThinkingLevelsFor()
	if len(got) != 1 || got[0] != "off" {
		t.Fatalf("%v", got)
	}
	if ClampThinking("high", m) != "off" {
		t.Fatalf("clamp=%s", ClampThinking("high", m))
	}
}

func TestThinkingLevelsExcludesUnmappedXHigh(t *testing.T) {
	m := Model{Reasoning: boolPtr(true)}
	got := m.ThinkingLevelsFor()
	for _, l := range got {
		if l == "xhigh" || l == "max" {
			t.Fatalf("unexpected %s in %v", l, got)
		}
	}
	m.ThinkingLevelMap = map[string]*string{"xhigh": strPtr("xhigh"), "max": strPtr("max")}
	got = m.ThinkingLevelsFor()
	found := false
	for _, l := range got {
		if l == "xhigh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing xhigh: %v", got)
	}
}

func TestThinkingLevelsNullUnsupported(t *testing.T) {
	m := Model{Reasoning: boolPtr(true), ThinkingLevelMap: map[string]*string{"minimal": nil}}
	for _, l := range m.ThinkingLevelsFor() {
		if l == "minimal" {
			t.Fatal("minimal should be excluded")
		}
	}
}

func TestSupportsImage(t *testing.T) {
	if !(Model{}).SupportsImage() {
		t.Fatal("empty input should allow images")
	}
	if (Model{Input: []string{"text"}}).SupportsImage() {
		t.Fatal("text-only should not allow images")
	}
	if !(Model{Input: []string{"text", "image"}}).SupportsImage() {
		t.Fatal("image input should allow images")
	}
}

func TestNextThinkingLevelIn(t *testing.T) {
	if NextThinkingLevelIn("off", []string{"off", "low", "high"}) != "low" {
		t.Fatal("next")
	}
	if NextThinkingLevelIn("high", []string{"off", "low", "high"}) != "off" {
		t.Fatal("wrap")
	}
}
