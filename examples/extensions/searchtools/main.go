// Command searchtools is a deferred-tool loader example. It registers
// tool_search (always intended to stay active) and lookup (activated on
// demand). Fireworks Messages then send lookup with defer_loading.
//
//	go build -o /tmp/searchtools-ext ./examples/extensions/searchtools
//	go run ./cmd/pigo -e /tmp/searchtools-ext -p "look up alpha"
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Lowpower/pigo/internal/ext"
)

func main() {
	err := ext.Serve(ext.Handler{
		Name: "searchtools",
		Tools: []ext.ToolDef{
			{
				Name:        "tool_search",
				Description: "Find tools by keyword and activate matches",
				Schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string", "description": "Keyword"},
					},
					"required": []any{"query"},
				},
				Fn: func(_ context.Context, args map[string]any) (string, bool) {
					query, _ := args["query"].(string)
					if query == "" {
						return "query is required", true
					}
					if err := ext.SetActiveTools([]string{"tool_search", "lookup"}); err != nil {
						return err.Error(), true
					}
					return "Found lookup.", false
				},
			},
			{
				Name:        "lookup",
				Description: "Look up a synthetic key",
				Schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"key": map[string]any{"type": "string", "description": "Key to look up"},
					},
					"required": []any{"key"},
				},
				Fn: func(_ context.Context, args map[string]any) (string, bool) {
					key, _ := args["key"].(string)
					return fmt.Sprintf("value for %s", key), false
				},
			},
		},
		Events: []string{"session_start"},
		OnEvent: func(event string, _ map[string]any) map[string]any {
			if event == "session_start" {
				_ = ext.SetActiveTools([]string{"tool_search"})
			}
			return nil
		},
	})
	if err != nil {
		log.Fatalf("searchtools extension: %v", err)
	}
}
