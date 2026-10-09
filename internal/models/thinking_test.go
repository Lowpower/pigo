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

func TestProviderThinkingValue(t *testing.T) {
	m := Model{Reasoning: boolPtr(true), ThinkingLevelMap: map[string]*string{
		"xhigh":   strPtr("max"),
		"minimal": nil,
		"low":     strPtr("medium"),
		"off":     strPtr("none"),
		"high":    strPtr("  "),
	}}
	if v, send := m.ProviderThinkingValue("xhigh"); !send || v != "max" {
		t.Fatalf("xhigh = %q send=%v", v, send)
	}
	if v, send := m.ProviderThinkingValue("low"); !send || v != "medium" {
		t.Fatalf("low = %q send=%v", v, send)
	}
	if v, send := m.ProviderThinkingValue("minimal"); send || v != "" {
		t.Fatalf("null minimal = %q send=%v", v, send)
	}
	if v, send := m.ProviderThinkingValue("off"); !send || v != "none" {
		t.Fatalf("off = %q send=%v", v, send)
	}
	if v, send := m.ProviderThinkingValue("high"); send || v != "" {
		t.Fatalf("blank high = %q send=%v", v, send)
	}
	if v, send := m.ProviderThinkingValue("medium"); !send || v != "medium" {
		t.Fatalf("unmapped medium = %q send=%v", v, send)
	}

	offNull := Model{ThinkingLevelMap: map[string]*string{"off": nil}}
	if v, send := offNull.ProviderThinkingValue("off"); send || v != "" {
		t.Fatalf("off null = %q send=%v", v, send)
	}

	plain := Model{}
	if v, send := plain.ProviderThinkingValue("high"); !send || v != "high" {
		t.Fatalf("plain high = %q send=%v", v, send)
	}
	if v, send := plain.ProviderThinkingValue("off"); send || v != "" {
		t.Fatalf("plain off = %q send=%v", v, send)
	}
	if v, send := plain.ProviderThinkingValue(""); send || v != "" {
		t.Fatalf("plain empty = %q send=%v", v, send)
	}
}

func TestRequestThinkingLevelKeepsUnsupportedOff(t *testing.T) {
	m := Model{Reasoning: boolPtr(true), ThinkingLevelMap: map[string]*string{
		"off":     nil,
		"minimal": nil,
		"low":     strPtr("medium"),
		"xhigh":   strPtr("max"),
	}}
	if got := m.RequestThinkingLevel("off"); got != "off" {
		t.Fatalf("off clamped to %q", got)
	}
	if _, send := m.ProviderThinkingValue(m.RequestThinkingLevel("off")); send {
		t.Fatal("off:null should not send")
	}
	if got := m.RequestThinkingLevel("minimal"); got != "low" {
		t.Fatalf("minimal clamped to %q", got)
	}
	if v, send := m.ProviderThinkingValue(m.RequestThinkingLevel("minimal")); !send || v != "medium" {
		t.Fatalf("clamped minimal wire = %q send=%v", v, send)
	}
	if got := m.RequestThinkingLevel("xhigh"); got != "xhigh" {
		t.Fatalf("xhigh = %q", got)
	}
	if got := m.RequestThinkingLevel(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := (Model{Reasoning: boolPtr(false)}).RequestThinkingLevel("high"); got != "off" {
		t.Fatalf("non-reasoning = %q", got)
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
