package models

import "testing"

func TestAnthropicMidConvoToolChangesMatchPi130(t *testing.T) {
	both := []string{
		"claude-opus-4-8",
		"claude-opus-4.8",
		"claude-opus-5",
		"claude-opus-5-5",
		"claude-opus-5.5",
		"claude-sonnet-5-5",
		"claude-sonnet-5.5",
		"claude-haiku-5-5",
		"claude-haiku-5.5",
		"claude-fable-5",
		"claude-fable-5-1",
		"claude-fable-5.1",
		"claude-mythos-5",
		"claude-mythos-5-1",
		"claude-opus-4-8-20260101",
	}
	for _, id := range both {
		got := annotateBuiltin(Model{Provider: "anthropic", ID: id})
		if got.Compat == nil || !got.Compat.SupportsMidConvoSystemMessages || !got.Compat.SupportsMidConvoToolChanges {
			t.Fatalf("%s flags = %+v", id, got.Compat)
		}
	}
	for _, id := range []string{"claude-opus-4-7", "claude-sonnet-5", "claude-haiku-4-5", "claude-opus-4-8@default"} {
		got := annotateBuiltin(Model{Provider: "anthropic", ID: id})
		if got.Compat != nil && (got.Compat.SupportsMidConvoSystemMessages || got.Compat.SupportsMidConvoToolChanges) {
			t.Fatalf("%s should not be flagged: %+v", id, got.Compat)
		}
	}
	copilot := annotateBuiltin(Model{Provider: "github-copilot", ID: "claude-opus-4.8"})
	if copilot.Compat == nil || !copilot.Compat.SupportsMidConvoSystemMessages || copilot.Compat.SupportsMidConvoToolChanges {
		t.Fatalf("copilot = %+v", copilot.Compat)
	}
	open := annotateBuiltin(Model{Provider: "opencode", ID: "claude-opus-4-8"})
	if open.Compat == nil || !open.Compat.SupportsMidConvoSystemMessages || open.Compat.SupportsMidConvoToolChanges {
		t.Fatalf("opencode = %+v", open.Compat)
	}
	router := annotateBuiltin(Model{Provider: "openrouter", ID: "anthropic/claude-opus-5"})
	if router.Compat != nil && (router.Compat.SupportsMidConvoSystemMessages || router.Compat.SupportsMidConvoToolChanges) {
		t.Fatalf("openrouter = %+v", router.Compat)
	}
}

func TestCatalogAnthropicInlineToolFlags(t *testing.T) {
	want := []string{"claude-opus-4-8", "claude-opus-5", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5", "claude-fable-5-1"}
	for _, id := range want {
		m, ok := Lookup("anthropic", id)
		if !ok {
			t.Fatalf("missing anthropic/%s", id)
		}
		if m.Compat == nil || !m.Compat.SupportsMidConvoSystemMessages || !m.Compat.SupportsMidConvoToolChanges {
			t.Fatalf("anthropic/%s flags = %+v", id, m.Compat)
		}
	}
	if m, ok := Lookup("anthropic", "claude-opus-4-7"); ok && m.Compat != nil && m.Compat.SupportsMidConvoToolChanges {
		t.Fatalf("opus 4.7 tool changes = %+v", m.Compat)
	}
	if m, ok := Lookup("github-copilot", "claude-opus-4.8"); ok {
		if m.Compat == nil || !m.Compat.SupportsMidConvoSystemMessages || m.Compat.SupportsMidConvoToolChanges {
			t.Fatalf("copilot catalog = %+v", m.Compat)
		}
	}
	if m, ok := Lookup("opencode", "claude-opus-4-8"); ok {
		if m.Compat == nil || !m.Compat.SupportsMidConvoSystemMessages || m.Compat.SupportsMidConvoToolChanges {
			t.Fatalf("opencode catalog = %+v", m.Compat)
		}
	}
}
