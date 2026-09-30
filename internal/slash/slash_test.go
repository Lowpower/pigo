package slash

import "testing"

func TestParse(t *testing.T) {
	tests := []struct{ in, name, rest string }{
		{"/quit", "quit", ""},
		{"/exit", "quit", ""},
		{"/model anthropic/claude-sonnet-4", "model", "anthropic/claude-sonnet-4"},
		{"/clone", "clone", ""},
		{"/tree", "tree", ""},
		{"/image a red cube", "image", "a red cube"},
	}
	for _, tt := range tests {
		c, ok := Parse(tt.in)
		if !ok || c.Name != tt.name || c.Rest != tt.rest {
			t.Fatalf("Parse(%q) = %+v ok=%v", tt.in, c, ok)
		}
	}
	for _, in := range []string{"hello", "// comment"} {
		if _, ok := Parse(in); ok {
			t.Fatalf("%q should not parse as slash", in)
		}
	}
}

func TestHotkeysTextIncludesModelSelect(t *testing.T) {
	text := HotkeysText()
	if !contains(text, "ctrl+l") || !contains(text, "open model selector") {
		t.Fatalf("hotkeys missing model select:\n%s", text)
	}
}

func TestScopedModelsDescription(t *testing.T) {
	c, ok := Parse("/scoped-models")
	if !ok || c.Name != "scoped-models" {
		t.Fatalf("%+v ok=%v", c, ok)
	}
	if c.Description != "Enable/disable models for Ctrl+P cycling" {
		t.Fatalf("description = %q", c.Description)
	}
	if !contains(HelpText(), "Enable/disable models for Ctrl+P cycling") {
		t.Fatalf("help:\n%s", HelpText())
	}
}

func TestIsBuiltinAndHelpTextWith(t *testing.T) {
	if !IsBuiltin("help") || !IsBuiltin("quit") || IsBuiltin("cmd") {
		t.Fatal("builtin lookup")
	}
	text := HelpTextWith([]Command{{Name: "cmd", Description: "demo"}})
	if !contains(text, "/cmd") {
		t.Fatalf("help extra:\n%s", text)
	}
}

func TestHelpListsShareAndChangelog(t *testing.T) {
	text := HelpText()
	if !contains(text, "/share") || !contains(text, "gist") {
		t.Fatalf("help missing share:\n%s", text)
	}
	if !contains(text, "/trust") {
		t.Fatalf("help missing /trust:\n%s", text)
	}
	if !contains(text, "/llama") {
		t.Fatalf("help missing /llama:\n%s", text)
	}
	if !contains(text, "/image") {
		t.Fatalf("help missing /image:\n%s", text)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
