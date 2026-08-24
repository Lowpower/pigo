package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/slash"
)

// ServeRPC is a JSONL stdin/stdout subset of pi --mode rpc.
func (e *Engine) ServeRPC(ctx context.Context, in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	history := e.History()
	var cancel context.CancelFunc
	running := false
	emit := func(v any) { _ = enc.Encode(v) }
	emit(map[string]any{"type": "ready", "provider": e.Provider, "model": e.Opts.Config.ResolvedModel()})

	reply := func(id any, ok bool, extra map[string]any) {
		payload := map[string]any{"type": "response", "ok": ok}
		if id != nil {
			payload["id"] = id
		}
		for k, v := range extra {
			payload[k] = v
		}
		emit(payload)
	}

	for {
		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		typ, _ := raw["type"].(string)
		id := raw["id"]
		msg, _ := raw["message"].(string)

		switch typ {
		case "quit", "shutdown":
			return nil
		case "abort":
			if cancel != nil {
				cancel()
			}
			emit(map[string]any{"type": "aborted", "id": id})
		case "prompt":
			behavior, _ := raw["streamingBehavior"].(string)
			if running && behavior == "steer" {
				e.PushSteer(msg)
				reply(id, true, nil)
				continue
			}
			if running && behavior == "followUp" {
				e.PushFollow(msg)
				reply(id, true, nil)
				continue
			}
			if cancel != nil {
				cancel()
			}
			cctx, c := context.WithCancel(ctx)
			cancel = c
			running = true
			if err := e.PrintJSON(cctx, out, history, msg); err != nil {
				emit(map[string]any{"type": "error", "message": err.Error(), "id": id})
			}
			history = e.History()
			if len(history) == 0 {
				history = append(history, ai.Message{Role: ai.RoleUser, Content: msg})
			}
			running = false
		case "steer":
			e.PushSteer(msg)
			reply(id, true, nil)
		case "follow_up":
			e.PushFollow(msg)
			reply(id, true, nil)
		case "new_session":
			if e.Opts.Session != nil {
				e.Opts.Session = session.New(e.Opts.Cwd, e.Opts.AgentDir)
			}
			history = nil
			e.persisted = 0
			reply(id, true, nil)
		case "get_state":
			sid := ""
			sfile := ""
			if e.Opts.Session != nil {
				sid = e.Opts.Session.ID()
				sfile = e.Opts.Session.File()
			}
			reply(id, true, map[string]any{
				"provider": e.Provider,
				"model":    e.Opts.Config.ResolvedModel(),
				"thinking": e.Opts.Config.Thinking,
				"session":  sid,
				"file":     sfile,
				"running":  running,
			})
		case "set_model":
			provider, _ := raw["provider"].(string)
			modelID, _ := raw["modelId"].(string)
			if provider != "" {
				e.Opts.Config.Provider = provider
				e.Opts.Config.DefaultProvider = provider
			}
			if modelID != "" {
				e.Opts.Config.Model = modelID
				e.Opts.Config.DefaultModel = modelID
			}
			reply(id, true, map[string]any{"provider": e.Opts.Config.ResolvedProvider(), "model": e.Opts.Config.ResolvedModel()})
		case "get_available_models":
			var list []map[string]string
			for _, m := range models.Catalog() {
				list = append(list, map[string]string{"provider": m.Provider, "id": m.ID, "api": m.API})
			}
			reply(id, true, map[string]any{"models": list})
		case "set_thinking_level":
			level, _ := raw["level"].(string)
			e.Opts.Config.Thinking = level
			reply(id, true, map[string]any{"thinking": level})
		case "get_available_thinking_levels":
			reply(id, true, map[string]any{"levels": []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}})
		case "set_steering_mode":
			mode, _ := raw["mode"].(string)
			e.Opts.Config.SteeringMode = mode
			reply(id, true, nil)
		case "set_follow_up_mode":
			mode, _ := raw["mode"].(string)
			e.Opts.Config.FollowUpMode = mode
			reply(id, true, nil)
		case "compact":
			outHist, summary, err := e.MaybeCompact(ctx, history)
			if err != nil {
				reply(id, false, map[string]any{"error": err.Error()})
				break
			}
			history = outHist
			reply(id, true, map[string]any{"summary": summary})
		case "set_auto_compaction":
			enabled, _ := raw["enabled"].(bool)
			on := enabled
			e.Opts.Config.CompactionOn = &on
			reply(id, true, nil)
		case "bash":
			cmdStr, _ := raw["command"].(string)
			result, isErr := e.Tools.Execute(ctx, "bash", map[string]any{"command": cmdStr})
			reply(id, !isErr, map[string]any{"result": result, "isError": isErr})
		case "get_messages":
			reply(id, true, map[string]any{"messages": history})
		case "get_last_assistant_text":
			text := lastAssistantText(history)
			reply(id, true, map[string]any{"text": text})
		case "set_session_name":
			name, _ := raw["name"].(string)
			if e.Opts.Session != nil {
				e.Opts.Session.SetName(name)
			}
			reply(id, true, nil)
		case "switch_session":
			path, _ := raw["sessionPath"].(string)
			m, err := session.Open(path)
			if err != nil {
				reply(id, false, map[string]any{"error": err.Error()})
				break
			}
			e.Opts.Session = m
			history = e.History()
			reply(id, true, map[string]any{"session": m.ID()})
		case "clone":
			if e.Opts.Session == nil {
				reply(id, false, map[string]any{"error": "no session"})
				break
			}
			child, err := e.Opts.Session.Fork(e.Opts.Cwd, e.Opts.AgentDir)
			if err != nil {
				reply(id, false, map[string]any{"error": err.Error()})
				break
			}
			e.Opts.Session = child
			e.persisted = len(child.Entries())
			reply(id, true, map[string]any{"session": child.ID(), "file": child.File()})
		case "get_commands":
			var cmds []map[string]string
			for _, c := range slash.Builtins() {
				cmds = append(cmds, map[string]string{"name": c.Name, "description": c.Description})
			}
			reply(id, true, map[string]any{"commands": cmds})
		case "get_session_stats":
			n := 0
			if e.Opts.Session != nil {
				n = len(e.Opts.Session.Entries())
			}
			reply(id, true, map[string]any{"entries": n, "messages": len(history)})
		default:
			emit(map[string]any{"type": "error", "id": id, "message": "unsupported rpc command: " + typ + " (see docs/parity-gaps.md)"})
		}
	}
}

func lastAssistantText(msgs []ai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.RoleAssistant {
			return msgs[i].Text()
		}
	}
	return ""
}

// silence unused in case tool names differ in some builds
var _ = exec.Command
var _ = os.Stderr
var _ = fmt.Sprintf
var _ = strings.TrimSpace
