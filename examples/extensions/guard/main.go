// Command guard claims a --plan flag, registers a shortcut, and prefixes
// user input when the flag is set.
//
//	go build -o /tmp/guard-ext ./examples/extensions/guard
//	go run ./cmd/pigo -e /tmp/guard-ext --plan -p "summarize this"
package main

import (
	"log"

	"github.com/Lowpower/pigo/internal/ext"
)

func main() {
	err := ext.Serve(ext.Handler{
		Name: "guard",
		Flags: []ext.FlagDef{{
			Name:        "plan",
			Description: "prefix prompts with a planning instruction",
			Type:        "boolean",
			Default:     false,
		}},
		Shortcuts: []ext.ShortcutDef{{
			Name:        "ctrl+shift+g",
			Description: "guard shortcut (no-op demo)",
			Fn:          func() {},
		}},
		Events: []string{"input"},
		OnEvent: func(event string, payload map[string]any) map[string]any {
			if event != "input" {
				return nil
			}
			v, ok := ext.Flag("plan")
			if !ok {
				return nil
			}
			on, _ := v.(bool)
			if !on {
				return nil
			}
			text, _ := payload["text"].(string)
			if text == "" {
				return nil
			}
			return map[string]any{
				"action": "transform",
				"text":   "Plan first, then act.\n\n" + text,
			}
		},
	})
	if err != nil {
		log.Fatalf("guard extension: %v", err)
	}
}
