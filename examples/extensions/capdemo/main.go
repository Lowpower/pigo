// Command capdemo is an extension that registers a slash command, blocks tool
// calls, and serves a scripted provider stream (no network).
//
//	go build -o /tmp/capdemo ./examples/extensions/capdemo
//	go run ./cmd/pigo -e /tmp/capdemo --provider capdemo --model demo -p "hi"
package main

import (
	"log"

	"github.com/Lowpower/pigo/internal/ext"
)

func main() {
	err := ext.Serve(ext.Handler{
		Name: "capdemo",
		Commands: []ext.CommandDef{{
			Name:        "cmd",
			Description: "demo slash command",
			Fn:          func(string) {},
		}},
		Events: []string{"tool_call"},
		OnEvent: func(event string, _ map[string]any) map[string]any {
			if event == "tool_call" {
				return map[string]any{"block": true, "reason": "blocked by capdemo"}
			}
			return nil
		},
		Providers: []ext.ProviderDef{{
			ID: "capdemo",
			Args: map[string]any{
				"name":   "capdemo",
				"stream": true,
				"models": []any{map[string]any{"id": "demo"}},
			},
		}},
		OnStream: func(_ map[string]any, emit func(event string, payload map[string]any), abort <-chan struct{}) {
			select {
			case <-abort:
				emit("error", map[string]any{"reason": "aborted"})
				return
			default:
			}
			emit("start", map[string]any{})
			emit("text_start", map[string]any{"contentIndex": 0})
			emit("text_delta", map[string]any{"contentIndex": 0, "delta": "hello from capdemo"})
			emit("text_end", map[string]any{"contentIndex": 0, "content": "hello from capdemo"})
			emit("done", map[string]any{
				"message": map[string]any{
					"role":       "assistant",
					"stopReason": "stop",
					"content":    []any{map[string]any{"type": "text", "text": "hello from capdemo"}},
				},
			})
		},
	})
	if err != nil {
		log.Fatalf("capdemo: %v", err)
	}
}
