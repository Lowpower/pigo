// Command hello is a minimal custom-tool extension. The host (internal/ext)
// spawns it and talks to it over stdin/stdout using the extension RPC protocol.
// It registers one tool, "hello", that greets a name.
//
//	go build -o /tmp/hello-ext ./examples/extensions/hello
//	go run ./cmd/pigo -e /tmp/hello-ext -p "say hello to pigo"
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Lowpower/pigo/internal/ext"
)

func main() {
	err := ext.Serve(ext.Handler{
		Name: "hello",
		Tools: []ext.ToolDef{{
			Name:        "hello",
			Description: "A simple greeting tool",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Name to greet"},
				},
				"required": []any{"name"},
			},
			Fn: func(_ context.Context, args map[string]any) (string, bool) {
				name, ok := args["name"].(string)
				if !ok {
					return "name must be a string", true
				}
				return fmt.Sprintf("Hello, %s!", name), false
			},
		}},
	})
	if err != nil {
		log.Fatalf("hello extension: %v", err)
	}
}
