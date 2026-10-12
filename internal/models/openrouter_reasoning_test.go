package models

import "testing"

func TestOpenRouterThinkingLevelMap(t *testing.T) {
	got := OpenRouterThinkingLevelMap(OpenRouterReasoning{
		Mandatory:        true,
		SupportedEfforts: []string{"max", "high", "low"},
	})
	expectLevel(t, got, "off", "", false)
	expectLevel(t, got, "minimal", "", false)
	expectLevel(t, got, "low", "low", true)
	expectLevel(t, got, "medium", "", false)
	expectLevel(t, got, "high", "high", true)
	expectLevel(t, got, "xhigh", "", false)
	expectLevel(t, got, "max", "max", true)

	mandatoryOnly := OpenRouterThinkingLevelMap(OpenRouterReasoning{Mandatory: true})
	if len(mandatoryOnly) != 1 {
		t.Fatalf("mandatory without efforts = %#v", mandatoryOnly)
	}
	expectLevel(t, mandatoryOnly, "off", "", false)

	optional := OpenRouterThinkingLevelMap(OpenRouterReasoning{
		SupportedEfforts: []string{"high", "low"},
	})
	expectLevel(t, optional, "off", "none", true)
	expectLevel(t, optional, "low", "low", true)
	expectLevel(t, optional, "high", "high", true)
	expectLevel(t, optional, "medium", "", false)
	expectLevel(t, optional, "max", "", false)

	if OpenRouterThinkingLevelMap(OpenRouterReasoning{}) != nil {
		t.Fatal("optional model without efforts should not invent a map")
	}
}

func TestOpenRouterCatalogAppliesReasoningMetadata(t *testing.T) {
	glm, ok := Lookup("openrouter", "z-ai/glm-5.2")
	if !ok {
		t.Fatal("missing z-ai/glm-5.2")
	}
	if glm.Compat == nil || glm.Compat.ThinkingFormat != "openrouter" {
		t.Fatalf("glm format = %+v", glm.Compat)
	}
	off := glm.ThinkingLevelMap["off"]
	if off == nil || *off != "none" {
		t.Fatalf("glm off = %v", off)
	}
	if glm.ThinkingLevelMap["low"] != nil {
		t.Fatal("glm low should be unsupported")
	}
	levels := glm.ThinkingLevelsFor()
	if stringsJoin(levels) != "off,high,xhigh" {
		t.Fatalf("glm levels = %v", levels)
	}

	step, ok := Lookup("openrouter", "stepfun/step-5-preview")
	if !ok {
		t.Fatal("missing stepfun/step-5-preview")
	}
	if step.Compat == nil || step.Compat.ThinkingFormat != "openrouter" {
		t.Fatalf("step format = %+v", step.Compat)
	}
	if mapped, present := step.ThinkingLevelMap["off"]; !present || mapped != nil {
		t.Fatalf("mandatory off = %v present=%v", mapped, present)
	}
	if stringsJoin(step.ThinkingLevelsFor()) != "low,medium,high" {
		t.Fatalf("step levels = %v", step.ThinkingLevelsFor())
	}
}

func stringsJoin(levels []string) string {
	out := ""
	for i, level := range levels {
		if i > 0 {
			out += ","
		}
		out += level
	}
	return out
}

func expectLevel(t *testing.T, m map[string]*string, level, want string, send bool) {
	t.Helper()
	got, ok := m[level]
	if !send {
		if !ok || got != nil {
			t.Fatalf("%s = %v present=%v, want null", level, got, ok)
		}
		return
	}
	if !ok || got == nil || *got != want {
		t.Fatalf("%s = %v, want %q", level, got, want)
	}
}
