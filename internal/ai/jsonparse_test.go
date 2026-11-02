package ai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseStreamingJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]any
	}{
		{"empty", "", map[string]any{}},
		{"whitespace", "   ", map[string]any{}},
		{"complete", `{"a":1,"b":"x"}`, map[string]any{"a": float64(1), "b": "x"}},
		{"incomplete string value", `{"path":"src/fo`, map[string]any{"path": "src/fo"}},
		{"incomplete object", `{"a":1`, map[string]any{"a": float64(1)}},
		{"incomplete nested object", `{"a":{"b":1`, map[string]any{"a": map[string]any{"b": float64(1)}}},
		{"incomplete array", `{"items":[1,2`, map[string]any{"items": []any{float64(1), float64(2)}}},
		{"trailing comma", `{"a":1,`, map[string]any{"a": float64(1)}},
		{"dangling key", `{"a":1,"b":`, map[string]any{"a": float64(1)}},
		{"garbage", `not json at all`, map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStreamingJSON(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseStreamingJSON(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseStreamingJSONRepairsControlChars(t *testing.T) {
	// A raw newline inside a string is invalid JSON; repair must escape it.
	got := parseStreamingJSON("{\"a\":\"x\ny\"}")
	if got["a"] != "x\ny" {
		t.Errorf(`a = %q, want "x\ny"`, got["a"])
	}
}

func TestRepairJSONInvalidEscape(t *testing.T) {
	// A lone backslash before an invalid escape char must be doubled.
	got := parseStreamingJSON(`{"a":"c:\path"}`)
	if got["a"] != `c:\path` {
		t.Errorf(`a = %q, want %q`, got["a"], `c:\path`)
	}
}

func TestParseStreamingJSONKeepsFieldsAfterInvalidUnicodeEscape(t *testing.T) {
	got := parseStreamingJSON(`{"path":"\usepackage{x}","count":1}`)
	if got["path"] != `\usepackage{x}` {
		t.Errorf(`path = %q, want %q`, got["path"], `\usepackage{x}`)
	}
	if got["count"] != float64(1) {
		t.Errorf("count = %#v, want 1", got["count"])
	}
}

func TestParseStreamingJSONKeepsUnicodeEscape(t *testing.T) {
	got := parseStreamingJSON(`{"a":"\u0041"}`)
	if got["a"] != "A" {
		t.Errorf(`a = %q, want "A"`, got["a"])
	}
}

func TestParseStreamingJSONStillCompletesOpenObject(t *testing.T) {
	got := parseStreamingJSON(`{"path":"/tmp/x"`)
	if got["path"] != "/tmp/x" {
		t.Errorf(`path = %q, want "/tmp/x"`, got["path"])
	}
}

func TestParseToolCallArguments(t *testing.T) {
	raw := `{"path":"\usepackage{x}","count":1}`
	args, errText := parseToolCallArguments(raw)
	if errText != "" {
		t.Fatalf("err = %q", errText)
	}
	if args["path"] != `\usepackage{x}` || args["count"] != float64(1) {
		t.Fatalf("args = %#v", args)
	}

	if args, errText = parseToolCallArguments("  "); errText != "" || len(args) != 0 {
		t.Fatalf("blank = %#v %q", args, errText)
	}

	cut := `{"path":"/tmp/x"`
	args, errText = parseToolCallArguments(cut)
	if len(args) != 0 {
		t.Fatalf("truncated args = %#v, want empty", args)
	}
	if !strings.Contains(errText, "Received arguments:\n"+cut) {
		t.Fatalf("err = %q, want raw arguments", errText)
	}

	args, errText = parseToolCallArguments("[1,2]")
	if len(args) != 0 || !strings.Contains(errText, "Tool arguments must be a JSON object") || !strings.Contains(errText, "[1,2]") {
		t.Fatalf("array = %#v %q", args, errText)
	}
}

func TestFinalizeToolArguments(t *testing.T) {
	c := &Content{Type: KindToolCall, Arguments: map[string]any{"path": "a"}}
	finalizeToolArguments(c)
	if c.Arguments["path"] != "a" || c.ArgumentsError() != "" {
		t.Fatalf("structured = %#v %q", c.Arguments, c.ArgumentsError())
	}

	cut := `{"path":"/tmp/x"`
	c.partialJSON = cut
	finalizeToolArguments(c)
	if len(c.Arguments) != 0 || !strings.Contains(c.ArgumentsError(), "Received arguments:\n"+cut) {
		t.Fatalf("truncated = %#v %q", c.Arguments, c.ArgumentsError())
	}
	if c.partialJSON != "" {
		t.Fatalf("partialJSON = %q", c.partialJSON)
	}
}

func TestArgumentsErrorIsNotSerialized(t *testing.T) {
	c := &Content{Type: KindToolCall, ToolName: "read", argumentsError: "boom\n\nReceived arguments:\n{"}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "boom") || strings.Contains(string(b), "argumentsError") {
		t.Fatalf("serialized = %s", b)
	}
}
