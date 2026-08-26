package runtime

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/slash"
)

const rpcAlreadyStreaming = "Agent is already processing. Specify streamingBehavior ('steer' or 'followUp') to queue the message."

// ServeRPC is a JSONL stdin/stdout RPC mode.
func (e *Engine) ServeRPC(ctx context.Context, in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	var (
		encMu       sync.Mutex
		stateMu     sync.Mutex
		wg          sync.WaitGroup
		history     = e.History()
		cancel      context.CancelFunc
		running     bool
		bashCancels []context.CancelFunc
	)
	done := make(chan struct{})
	defer close(done)
	emit := func(v any) {
		encMu.Lock()
		defer encMu.Unlock()
		_ = enc.Encode(v)
	}
	setUI, onUIResponse, closeUI := newUIBridge(emit, done)
	setUI(e)
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		h.SetUI(func(method string, args map[string]any, timeout time.Duration) map[string]any {
			return e.RequestExtensionUI(method, args, timeout)
		})
	}
	defer func() {
		e.setUIHandler(nil)
		closeUI()
	}()
	e.onSessionEvent = emit
	defer func() { e.onSessionEvent = nil }()
	emit(map[string]any{"type": "ready", "provider": e.Provider, "model": e.Opts.Config.ResolvedModel()})

	reply := func(id any, command string, success bool, data any, errStr string) {
		payload := map[string]any{"type": "response", "command": command, "success": success}
		if id != nil {
			payload["id"] = id
		}
		if data != nil {
			payload["data"] = data
		}
		if errStr != "" {
			payload["error"] = errStr
		}
		emit(payload)
	}

	for {
		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			wg.Wait()
			if err == io.EOF {
				return nil
			}
			return err
		}
		typ, _ := raw["type"].(string)
		id := raw["id"]
		msg, _ := raw["message"].(string)

		if typ == "extension_ui_response" {
			onUIResponse(raw)
			continue
		}

		switch typ {
		case "quit", "shutdown":
			wg.Wait()
			return nil
		case "abort":
			e.AbortRetry()
			stateMu.Lock()
			if cancel != nil {
				cancel()
			}
			stateMu.Unlock()
			reply(id, "abort", true, nil, "")
		case "prompt":
			behavior, _ := raw["streamingBehavior"].(string)
			imgs := parseRPCImages(raw)
			stateMu.Lock()
			isRunning := running
			stateMu.Unlock()
			if isRunning && behavior == "steer" {
				e.PushSteerImages(msg, imgs)
				reply(id, "prompt", true, nil, "")
				continue
			}
			if isRunning && behavior == "followUp" {
				e.PushFollowImages(msg, imgs)
				reply(id, "prompt", true, nil, "")
				continue
			}
			if isRunning {
				reply(id, "prompt", false, nil, rpcAlreadyStreaming)
				continue
			}
			stateMu.Lock()
			if cancel != nil {
				cancel()
			}
			cctx, c := context.WithCancel(ctx)
			cancel = c
			running = true
			hist := history
			stateMu.Unlock()
			reply(id, "prompt", true, nil, "")
			wg.Add(1)
			go func(msg string, hist []ai.Message, imgs []ai.ImageContent, cctx context.Context, id any) {
				defer wg.Done()
				defer func() {
					stateMu.Lock()
					running = false
					cancel = nil
					history = e.History()
					if len(history) == 0 {
						history = append(append([]ai.Message(nil), hist...), ai.Message{Role: ai.RoleUser, Content: msg, Images: imgs})
					}
					stateMu.Unlock()
				}()
				if err := e.PrintJSON(cctx, out, hist, msg, imgs); err != nil {
					emit(map[string]any{"type": "error", "message": err.Error(), "id": id})
				}
			}(msg, hist, imgs, cctx, id)
		case "steer":
			e.PushSteerImages(msg, parseRPCImages(raw))
			reply(id, "steer", true, nil, "")
		case "follow_up":
			e.PushFollowImages(msg, parseRPCImages(raw))
			reply(id, "follow_up", true, nil, "")
		case "new_session":
			parent, _ := raw["parentSession"].(string)
			if e.Opts.Session != nil {
				e.Opts.Session = session.New(e.Opts.Cwd, e.Opts.AgentDir)
				if parent != "" {
					e.Opts.Session.SetParentSession(parent)
				}
			}
			stateMu.Lock()
			history = nil
			e.persisted = 0
			stateMu.Unlock()
			reply(id, "new_session", true, map[string]any{"cancelled": false}, "")
		case "get_state":
			sid := ""
			sfile := ""
			sname := ""
			if e.Opts.Session != nil {
				sid = e.Opts.Session.ID()
				sfile = e.Opts.Session.File()
				sname = e.Opts.Session.Name()
			}
			stateMu.Lock()
			isStreaming := running
			nmsg := len(history)
			stateMu.Unlock()
			reply(id, "get_state", true, map[string]any{
				"model":                 map[string]string{"provider": e.Opts.Config.ResolvedProvider(), "id": e.Opts.Config.ResolvedModel()},
				"thinkingLevel":         e.Opts.Config.Thinking,
				"isStreaming":           isStreaming,
				"isCompacting":          e.isCompacting(),
				"steeringMode":          queueMode(e.Opts.Config.SteeringMode),
				"followUpMode":          queueMode(e.Opts.Config.FollowUpMode),
				"sessionFile":           sfile,
				"sessionId":             sid,
				"sessionName":           sname,
				"autoCompactionEnabled": e.Opts.Config.CompactionEnabled(),
				"messageCount":          nmsg,
				"pendingMessageCount":   e.pendingCount(),
			}, "")
		case "set_model":
			provider, _ := raw["provider"].(string)
			modelID, _ := raw["modelId"].(string)
			e.ApplyModel(provider, modelID, "")
			reply(id, "set_model", true, map[string]any{"provider": e.Opts.Config.ResolvedProvider(), "id": e.Opts.Config.ResolvedModel()}, "")
		case "cycle_model":
			next, ok := e.CycleModel(false)
			if !ok {
				reply(id, "cycle_model", true, nil, "")
				break
			}
			reply(id, "cycle_model", true, map[string]any{
				"model":         map[string]string{"provider": next.Provider, "id": next.ID},
				"thinkingLevel": e.Opts.Config.Thinking,
				"isScoped":      len(e.Scoped) > 0,
			}, "")
		case "get_available_models":
			var list []map[string]string
			for _, m := range models.Catalog() {
				list = append(list, map[string]string{"provider": m.Provider, "id": m.ID, "api": m.API})
			}
			reply(id, "get_available_models", true, map[string]any{"models": list}, "")
		case "set_thinking_level":
			level, _ := raw["level"].(string)
			e.Opts.Config.Thinking = level
			emit(map[string]any{"type": "thinking_level_changed", "level": level})
			reply(id, "set_thinking_level", true, nil, "")
		case "cycle_thinking_level":
			level := e.CycleThinking()
			emit(map[string]any{"type": "thinking_level_changed", "level": level})
			reply(id, "cycle_thinking_level", true, map[string]any{"level": level}, "")
		case "get_available_thinking_levels":
			reply(id, "get_available_thinking_levels", true, map[string]any{"levels": models.ThinkingLevels}, "")
		case "set_steering_mode":
			mode, _ := raw["mode"].(string)
			e.Opts.Config.SteeringMode = mode
			reply(id, "set_steering_mode", true, nil, "")
		case "set_follow_up_mode":
			mode, _ := raw["mode"].(string)
			e.Opts.Config.FollowUpMode = mode
			reply(id, "set_follow_up_mode", true, nil, "")
		case "compact":
			custom, _ := raw["customInstructions"].(string)
			stateMu.Lock()
			hist := history
			stateMu.Unlock()
			outHist, summary, err := e.CompactNow(ctx, hist, custom)
			if err != nil {
				reply(id, "compact", false, nil, err.Error())
				break
			}
			stateMu.Lock()
			history = outHist
			stateMu.Unlock()
			reply(id, "compact", true, map[string]any{"summary": summary}, "")
		case "set_auto_compaction":
			enabled, _ := raw["enabled"].(bool)
			on := enabled
			e.Opts.Config.CompactionOn = &on
			reply(id, "set_auto_compaction", true, nil, "")
		case "bash":
			cmdStr, _ := raw["command"].(string)
			exclude, _ := raw["excludeFromContext"].(bool)
			bctx, bcancel := context.WithCancel(ctx)
			stateMu.Lock()
			bashCancels = append(bashCancels, bcancel)
			stateMu.Unlock()
			wg.Add(1)
			go func(id any, cmdStr string, exclude bool, bctx context.Context, bcancel context.CancelFunc) {
				defer wg.Done()
				defer bcancel()
				result := executeRPCBash(bctx, e.Opts.Cwd, cmdStr, func(delta string) {
					emit(map[string]any{"type": "bash_execution_update", "id": id, "delta": delta})
				})
				payload := map[string]any{
					"role":      "bashExecution",
					"command":   cmdStr,
					"output":    result.Output,
					"cancelled": result.Cancelled,
					"truncated": result.Truncated,
					"timestamp": time.Now().UnixMilli(),
				}
				if result.ExitCode != nil {
					payload["exitCode"] = *result.ExitCode
				}
				if result.FullOutputPath != "" {
					payload["fullOutputPath"] = result.FullOutputPath
				}
				if exclude {
					payload["excludeFromContext"] = true
				}
				if e.Opts.Session != nil {
					if entry, err := e.Opts.Session.AppendMessage("bashExecution", payload); err == nil && entry != nil {
						emit(map[string]any{"type": "entry_appended", "entry": entry})
					}
				}
				if !exclude {
					text := session.BashContextText(cmdStr, result.Output, result.Cancelled, result.ExitCode, result.Truncated, result.FullOutputPath)
					stateMu.Lock()
					history = append(history, ai.Message{Role: ai.RoleUser, Content: text})
					stateMu.Unlock()
				}
				reply(id, "bash", true, result.asData(), "")
			}(id, cmdStr, exclude, bctx, bcancel)
		case "get_messages":
			stateMu.Lock()
			hist := history
			stateMu.Unlock()
			reply(id, "get_messages", true, map[string]any{"messages": hist}, "")
		case "get_last_assistant_text":
			stateMu.Lock()
			hist := history
			stateMu.Unlock()
			text := lastAssistantText(hist)
			var data any
			if text == "" {
				data = map[string]any{"text": nil}
			} else {
				data = map[string]any{"text": text}
			}
			reply(id, "get_last_assistant_text", true, data, "")
		case "set_session_name":
			name, _ := raw["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				reply(id, "set_session_name", false, nil, "Session name cannot be empty")
				break
			}
			if e.Opts.Session != nil {
				e.Opts.Session.SetName(name)
			}
			emit(map[string]any{"type": "session_info_changed", "name": name})
			reply(id, "set_session_name", true, nil, "")
		case "switch_session":
			path, _ := raw["sessionPath"].(string)
			m, err := session.Open(path)
			if err != nil {
				reply(id, "switch_session", false, nil, err.Error())
				break
			}
			e.AdoptSession(m)
			stateMu.Lock()
			history = e.History()
			stateMu.Unlock()
			reply(id, "switch_session", true, map[string]any{"cancelled": false, "session": m.ID()}, "")
		case "clone":
			if e.Opts.Session == nil {
				reply(id, "clone", false, nil, "no session")
				break
			}
			child, err := e.Opts.Session.Fork(e.Opts.Cwd, e.Opts.AgentDir)
			if err != nil {
				reply(id, "clone", false, nil, err.Error())
				break
			}
			e.AdoptSession(child)
			stateMu.Lock()
			history = e.History()
			stateMu.Unlock()
			reply(id, "clone", true, map[string]any{"cancelled": false, "session": child.ID(), "file": child.File()}, "")
		case "fork":
			entryID, _ := raw["entryId"].(string)
			if e.Opts.Session == nil {
				reply(id, "fork", false, nil, "no session")
				break
			}
			child, text, err := e.Opts.Session.ForkFrom(entryID, e.Opts.Cwd, e.Opts.AgentDir, "before")
			if err != nil {
				reply(id, "fork", false, nil, err.Error())
				break
			}
			e.AdoptSession(child)
			stateMu.Lock()
			history = e.History()
			stateMu.Unlock()
			reply(id, "fork", true, map[string]any{"text": text, "cancelled": false}, "")
		case "get_fork_messages":
			var messages []map[string]string
			if e.Opts.Session != nil {
				messages = e.Opts.Session.UserMessagesForForking()
			}
			if messages == nil {
				messages = []map[string]string{}
			}
			reply(id, "get_fork_messages", true, map[string]any{"messages": messages}, "")
		case "get_entries":
			if e.Opts.Session == nil {
				reply(id, "get_entries", true, map[string]any{"entries": []session.Entry{}, "leafId": nil}, "")
				break
			}
			entries := e.Opts.Session.Entries()
			if since, _ := raw["since"].(string); since != "" {
				idx := -1
				for i, en := range entries {
					if en.ID == since {
						idx = i
						break
					}
				}
				if idx < 0 {
					reply(id, "get_entries", false, nil, "Entry not found: "+since)
					break
				}
				entries = entries[idx+1:]
			}
			var leaf any
			if id := e.Opts.Session.LeafID(); id != "" {
				leaf = id
			}
			reply(id, "get_entries", true, map[string]any{"entries": entries, "leafId": leaf}, "")
		case "get_tree":
			var tree []session.TreeNode
			var leaf any
			if e.Opts.Session != nil {
				tree = e.Opts.Session.GetTree()
				if id := e.Opts.Session.LeafID(); id != "" {
					leaf = id
				}
			}
			if tree == nil {
				tree = []session.TreeNode{}
			}
			reply(id, "get_tree", true, map[string]any{"tree": tree, "leafId": leaf}, "")
		case "export_html":
			if e.Opts.Session == nil {
				reply(id, "export_html", false, nil, "no session")
				break
			}
			outPath, _ := raw["outputPath"].(string)
			path, err := session.ExportHTML(e.Opts.Session, outPath)
			if err != nil {
				reply(id, "export_html", false, nil, err.Error())
				break
			}
			reply(id, "export_html", true, map[string]any{"path": path}, "")
		case "get_commands":
			var cmds []map[string]string
			for _, c := range slash.Builtins() {
				cmds = append(cmds, map[string]string{"name": c.Name, "description": c.Description, "source": "builtin"})
			}
			for _, t := range e.Templates {
				cmds = append(cmds, map[string]string{"name": t.Name, "description": t.Description, "source": "prompt"})
			}
			for _, s := range e.Skills {
				cmds = append(cmds, map[string]string{"name": s.Name, "description": s.Description, "source": "skill"})
			}
			reply(id, "get_commands", true, map[string]any{"commands": cmds}, "")
		case "get_session_stats":
			stateMu.Lock()
			hist := history
			stateMu.Unlock()
			stats := session.CollectStats(e.Opts.Session, hist, e.contextWindow())
			reply(id, "get_session_stats", true, stats, "")
		case "abort_bash":
			stateMu.Lock()
			cancels := append([]context.CancelFunc(nil), bashCancels...)
			stateMu.Unlock()
			for _, c := range cancels {
				c()
			}
			reply(id, "abort_bash", true, nil, "")
		case "set_auto_retry":
			enabled, _ := raw["enabled"].(bool)
			e.SetAutoRetryEnabled(enabled)
			reply(id, "set_auto_retry", true, nil, "")
		case "abort_retry":
			e.AbortRetry()
			reply(id, "abort_retry", true, nil, "")
		default:
			emit(map[string]any{"type": "error", "id": id, "message": "unsupported rpc command: " + typ})
		}
	}
}

func parseRPCImages(raw map[string]any) []ai.ImageContent {
	v, ok := raw["images"]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []ai.ImageContent
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		typ, _ := m["type"].(string)
		data, _ := m["data"].(string)
		mime, _ := m["mimeType"].(string)
		if data == "" || mime == "" {
			continue
		}
		if typ != "" && typ != "image" {
			continue
		}
		out = append(out, ai.ImageContent{Type: "image", Data: data, MimeType: mime})
	}
	return out
}

func queueMode(s string) string {
	if strings.EqualFold(s, "all") {
		return "all"
	}
	return "one-at-a-time"
}

func lastAssistantText(msgs []ai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.RoleAssistant {
			return msgs[i].Text()
		}
	}
	return ""
}
