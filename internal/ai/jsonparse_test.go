package ai

import (
	"reflect"
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
