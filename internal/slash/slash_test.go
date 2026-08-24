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
