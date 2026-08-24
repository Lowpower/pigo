// Command reverse is an example pigo extension. The host (internal/ext) spawns it
// and talks to it over stdin/stdout using the extension RPC protocol. It
// registers one tool, "reverse", that reverses a string.
//
// Build and use it by pointing the host at the compiled binary:
//
//	go build -o /tmp/reverse-ext ./examples/extensions/reverse
//	// host: ext.Spawn(ctx, "reverse", []string{"/tmp/reverse-ext"}, ext.Options{})
package main

import (
	"context"
	"log"

	"github.com/Lowpower/pigo/internal/ext"
)

func main() {
	err := ext.Serve(ext.Handler{
		Name: "reverse",
		Tools: []ext.ToolDef{{
			Name:        "reverse",
			Description: "Reverse a string",
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string", "description": "text to reverse"}},
				"required":   []any{"text"},
			},
			Fn: func(_ context.Context, args map[string]any) (string, bool) {
				s, ok := args["text"].(string)
				if !ok {
					return "text must be a string", true
				}
				r := []rune(s)
				for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
					r[i], r[j] = r[j], r[i]
				}
				return string(r), false
			},
		}},
	})
	if err != nil {
		log.Fatalf("reverse extension: %v", err)
	}
}
