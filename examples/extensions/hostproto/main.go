// Command hostproto shows HostCall / Notify / Status from an extension.
//
//	go build -o /tmp/hostproto-ext ./examples/extensions/hostproto
//	go run ./cmd/pigo -e /tmp/hostproto-ext -p "use host_echo"
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Lowpower/pigo/internal/ext"
)

func main() {
	err := ext.Serve(ext.Handler{
		Name:   "hostproto",
		Events: []string{"session_start"},
		OnEvent: func(event string, _ map[string]any) map[string]any {
			if event != "session_start" {
				return nil
			}
			info, err := ext.HostCall("session.info", nil)
			if err != nil {
				_ = ext.Notify("hostproto: "+err.Error(), "error")
				return nil
			}
			_ = ext.Status("hostproto", "host protocol")
			_ = ext.Notify(fmt.Sprintf("cwd=%v mode=%v", info["cwd"], info["mode"]), "info")
			return nil
		},
		Commands: []ext.CommandDef{{
			Name:        "hostinfo",
			Description: "notify a host session snapshot",
			Fn: func(string) {
				info, err := ext.HostCall("session.info", nil)
				if err != nil {
					_ = ext.Notify(err.Error(), "error")
					return
				}
				_ = ext.Notify(fmt.Sprintf("session %v", info["id"]), "info")
			},
		}},
		Tools: []ext.ToolDef{{
			Name:        "host_echo",
			Description: "Run echo via the host exec helper",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string", "description": "text to echo"},
				},
				"required": []any{"text"},
			},
			Fn: func(_ context.Context, args map[string]any) (string, bool) {
				text, _ := args["text"].(string)
				got, err := ext.HostCall("exec", map[string]any{
					"command": "echo", "args": []any{text},
				})
				if err != nil {
					return err.Error(), true
				}
				out, _ := got["stdout"].(string)
				return out, false
			},
		}},
	})
	if err != nil {
		log.Fatalf("hostproto: %v", err)
	}
}
