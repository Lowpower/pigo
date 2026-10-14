package models

import "testing"

func TestAnnotateBuiltinFillsMissingOpus55Effort(t *testing.T) {
	anthropic := annotateBuiltin(Model{Provider: "anthropic", ID: "claude-opus-5-5"})
	copilot := annotateBuiltin(Model{Provider: "github-copilot", ID: "claude-opus-5.5"})
	assertOpus55EffortFallback(t, anthropic.ThinkingLevelMap)
	assertOpus55EffortFallback(t, copilot.ThinkingLevelMap)

	other := annotateBuiltin(Model{Provider: "anthropic", ID: "claude-sonnet-5-5"})
	if other.ThinkingLevelMap != nil {
		t.Fatalf("other model map = %#v", other.ThinkingLevelMap)
	}
}

func TestAnnotateBuiltinKeepsCompleteOpus55Effort(t *testing.T) {
	custom := map[string]*string{
		"off":     strPtr("none"),
		"minimal": strPtr("min"),
		"low":     strPtr("L"),
		"medium":  strPtr("M"),
		"high":    strPtr("H"),
		"xhigh":   strPtr("XH"),
		"max":     strPtr("MX"),
		"extra":   strPtr("keep"),
	}
	got := annotateBuiltin(Model{
		Provider:         "anthropic",
		ID:               "claude-opus-5-5",
		ThinkingLevelMap: custom,
	})
	assertThinkingLevelValues(t, got.ThinkingLevelMap, map[string]string{
		"off": "none", "minimal": "min", "low": "L", "medium": "M",
		"high": "H", "xhigh": "XH", "max": "MX", "extra": "keep",
	})
}

func TestMergeOverlayFillsMissingOpus55Effort(t *testing.T) {
	out := mergeOverlay("anthropic", nil, []Model{{ID: "claude-opus-5-5"}})
	if len(out) != 1 {
		t.Fatalf("len = %d", len(out))
	}
	assertOpus55EffortFallback(t, out[0].ThinkingLevelMap)

	partialHigh := "H"
	out = mergeOverlay("github-copilot", []Model{{
		Provider: "github-copilot",
		ID:       "claude-opus-5.5",
	}}, []Model{{
		ID:               "claude-opus-5.5",
		ThinkingLevelMap: map[string]*string{"high": &partialHigh},
	}})
	got := out[0].ThinkingLevelMap
	if got["high"] == nil || *got["high"] != "H" {
		t.Fatalf("high = %v", got["high"])
	}
	for _, level := range []string{"low", "medium", "xhigh", "max"} {
		if got[level] == nil || *got[level] != level {
			t.Fatalf("%s = %v", level, got[level])
		}
	}
	if _, ok := got["off"]; !ok || got["off"] != nil {
		t.Fatalf("off = %v present=%v", got["off"], ok)
	}
	if _, ok := got["minimal"]; !ok || got["minimal"] != nil {
		t.Fatalf("minimal = %v present=%v", got["minimal"], ok)
	}

	other := mergeOverlay("anthropic", nil, []Model{{ID: "claude-sonnet-4-5"}})
	if other[0].ThinkingLevelMap != nil {
		t.Fatalf("other overlay map = %#v", other[0].ThinkingLevelMap)
	}
}

func TestMergeOverlayKeepsCompleteOpus55Effort(t *testing.T) {
	custom := map[string]*string{
		"off":     strPtr("none"),
		"minimal": nil,
		"low":     strPtr("L"),
		"medium":  strPtr("M"),
		"high":    strPtr("H"),
		"xhigh":   strPtr("XH"),
		"max":     strPtr("MX"),
	}
	out := mergeOverlay("anthropic", []Model{{
		Provider: "anthropic",
		ID:       "claude-opus-5-5",
	}}, []Model{{
		ID:               "claude-opus-5-5",
		ThinkingLevelMap: custom,
	}})
	got := out[0].ThinkingLevelMap
	if _, ok := got["minimal"]; !ok || got["minimal"] != nil {
		t.Fatalf("minimal = %v present=%v", got["minimal"], ok)
	}
	assertThinkingLevelValues(t, got, map[string]string{
		"off": "none", "low": "L", "medium": "M", "high": "H", "xhigh": "XH", "max": "MX",
	})
}

func assertOpus55EffortFallback(t *testing.T, got map[string]*string) {
	t.Helper()
	if len(got) != 7 {
		t.Fatalf("len = %d (%#v)", len(got), got)
	}
	if _, ok := got["off"]; !ok || got["off"] != nil {
		t.Fatalf("off = %v present=%v", got["off"], ok)
	}
	if _, ok := got["minimal"]; !ok || got["minimal"] != nil {
		t.Fatalf("minimal = %v present=%v", got["minimal"], ok)
	}
	for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
		if got[level] == nil || *got[level] != level {
			t.Fatalf("%s = %v", level, got[level])
		}
	}
}

func assertThinkingLevelValues(t *testing.T, got map[string]*string, want map[string]string) {
	t.Helper()
	for level, value := range want {
		if got[level] == nil || *got[level] != value {
			t.Fatalf("%s = %v, want %q", level, got[level], value)
		}
	}
}
