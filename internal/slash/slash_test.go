package slash

import "testing"

func TestParseQuit(t *testing.T) {
	c, ok := Parse("/quit")
	if !ok || c.Name != "quit" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
	c, ok = Parse("/exit")
	if !ok || c.Name != "quit" {
		t.Fatalf("alias exit: %+v ok=%v", c, ok)
	}
}

func TestParseArgs(t *testing.T) {
	c, ok := Parse("/model anthropic/claude-sonnet-4")
	if !ok || c.Name != "model" || c.Rest != "anthropic/claude-sonnet-4" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
}

func TestParseNonSlash(t *testing.T) {
	if _, ok := Parse("hello"); ok {
		t.Fatal("plain text should not parse as slash")
	}
	if _, ok := Parse("// comment"); ok {
		t.Fatal("// should not parse as slash")
	}
}

func TestParseCloneAndTree(t *testing.T) {
	c, ok := Parse("/clone")
	if !ok || c.Name != "clone" {
		t.Fatalf("clone: %+v ok=%v", c, ok)
	}
	c, ok = Parse("/tree")
	if !ok || c.Name != "tree" {
		t.Fatalf("tree: %+v ok=%v", c, ok)
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
