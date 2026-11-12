package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestBeforeAgentStartHelperProcess(_ *testing.T) {
	mode := os.Getenv("PIGO_AGENT_START")
	if mode == "" {
		return
	}
	mark := os.Getenv("PIGO_MARK")
	write := func(v any) {
		if mark == "" {
			return
		}
		b, _ := json.Marshal(v)
		f, err := os.OpenFile(mark, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	_ = ext.Serve(ext.Handler{
		Name:   "agent-start-ext",
		Events: []string{"before_agent_start"},
		OnEvent: func(_ string, payload map[string]any) map[string]any {
			write(payload)
			switch mode {
			case "join":
				msg, _ := payload["message"].(map[string]any)
				if msg["customType"] != "first" {
					return nil
				}
				idle, _ := ext.HostCall("isIdle", nil)
				write(map[string]any{"idle": idle["idle"]})
				userRes, _ := ext.HostCall("sendUserMessage", map[string]any{"content": "needs-deliver"})
				write(map[string]any{"userError": userRes["error"]})
				second, err := ext.HostCall("sendMessage", map[string]any{
					"customType": "second", "content": "task two finished",
					"display": true, "triggerTurn": true,
				})
				if err != nil {
					write(map[string]any{"secondError": err.Error()})
					return nil
				}
				write(map[string]any{"second": second})
			case "sys":
				return map[string]any{"systemPrompt": "KEEP-ME"}
			case "hookmsg":
				return map[string]any{"message": map[string]any{
					"customType": "note", "content": "from-hook", "display": true,
					"details": map[string]any{"k": "v"},
				}}
			case "abort":
				res, _ := ext.HostCall("abort", nil)
				write(res)
			}
			return nil
		},
	})
	os.Exit(0)
}

func spawnAgentStartExt(t *testing.T, mode, mark string) *ext.Host {
	t.Helper()
	h, err := ext.Spawn(context.Background(), "agent-start-ext",
		[]string{os.Args[0], "-test.run=^TestBeforeAgentStartHelperProcess$"},
		ext.Options{Env: []string{"PIGO_AGENT_START=" + mode, "PIGO_MARK=" + mark}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func readMark(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("mark %s: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestBeforeAgentStartUserMessageType(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	h := spawnAgentStartExt(t, "payload", mark)
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Config: config.Config{Model: "x"}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			return textReply("ok")(ctx, req, opts)
		},
	}
	e.bindExtensionHooks()
	wireQueues(e)
	_ = e.RunPrompt(context.Background(), nil, "hello", nil).Collect()
	ev := readMark(t, mark)[0]
	if ev["messageType"] != "user" || ev["prompt"] != "hello" {
		t.Fatalf("user event = %v", ev)
	}
}

func TestBeforeAgentStartCustomTrigger(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	h := spawnAgentStartExt(t, "payload", mark)
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	var seen []string
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			for _, m := range req.Messages {
				if m.Role == ai.RoleUser {
					seen = append(seen, m.Content)
				}
			}
			return textReply("ok")(ctx, req, opts)
		},
	}
	e.bindExtensionHooks()
	wireQueues(e)
	hist := e.History()
	e.SetKick(func(string, []ai.ImageContent) {
		_ = e.RunPrompt(context.Background(), hist, "", nil).Collect()
	})
	got := e.HandleHostCall(h, "sendMessage", map[string]any{
		"customType": "task-done", "content": "background task finished",
		"display": true, "triggerTurn": true, "details": map[string]any{"n": 1.0},
	})
	if got["error"] != nil {
		t.Fatalf("sendMessage = %v", got)
	}
	ev := readMark(t, mark)[0]
	if ev["messageType"] != "custom" {
		t.Fatalf("messageType = %v", ev["messageType"])
	}
	if _, ok := ev["prompt"]; ok {
		t.Fatalf("custom event has prompt: %v", ev)
	}
	msg, _ := ev["message"].(map[string]any)
	if msg["customType"] != "task-done" || msg["content"] != "background task finished" {
		t.Fatalf("message = %v", ev["message"])
	}
	if strings.Join(seen, "|") != "background task finished" {
		t.Fatalf("model messages = %v", seen)
	}
	var customN, userN int
	for _, ent := range sess.Entries() {
		switch ent.Type {
		case "custom_message":
			if string(ent.Content) == `"background task finished"` {
				customN++
				if !strings.Contains(string(ent.Details), `"n"`) {
					t.Fatalf("details = %s", ent.Details)
				}
			}
		case "message":
			if strings.Contains(string(ent.Message), "background task finished") {
				userN++
			}
		}
	}
	if customN != 1 || userN != 0 {
		t.Fatalf("custom=%d user=%d", customN, userN)
	}
}

func TestBeforeAgentStartSecondSendJoinsRun(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	h := spawnAgentStartExt(t, "join", mark)
	var requests [][]string
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Config: config.Config{Model: "x"}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			var texts []string
			for _, m := range req.Messages {
				if m.Role == ai.RoleUser && m.Content != "" {
					texts = append(texts, m.Content)
				}
			}
			requests = append(requests, texts)
			return textReply("ok")(ctx, req, opts)
		},
	}
	e.bindExtensionHooks()
	wireQueues(e)
	e.SetKick(func(string, []ai.ImageContent) {
		_ = e.RunPrompt(context.Background(), nil, "", nil).Collect()
	})
	_ = e.HandleHostCall(h, "sendMessage", map[string]any{
		"customType": "first", "content": "task one finished",
		"display": true, "triggerTurn": true,
	})
	if len(requests) != 1 {
		t.Fatalf("requests = %d %v", len(requests), requests)
	}
	if strings.Join(requests[0], "|") != "task one finished|task two finished" {
		t.Fatalf("first request = %v", requests[0])
	}
	rows := readMark(t, mark)
	var sawIdle, sawUserErr, sawSecond bool
	for _, row := range rows {
		if row["idle"] == false {
			sawIdle = true
		}
		if s, _ := row["userError"].(string); strings.Contains(s, "deliverAs") {
			sawUserErr = true
		}
		if second, ok := row["second"].(map[string]any); ok && second["error"] == nil && second["ok"] == true {
			sawSecond = true
		}
	}
	if !sawIdle || !sawUserErr || !sawSecond {
		t.Fatalf("hook log = %v", rows)
	}
	if !e.IsIdle() {
		t.Fatal("run left the engine busy")
	}
}

func TestBeforeAgentStartSystemPromptSurvivesSecondRequest(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	h := spawnAgentStartExt(t, "sys", mark)
	var systems []string
	e := &Engine{
		Hosts: []*ext.Host{h},
		Tools: tools.NewRegistry(streamyTool{}),
		Opts:  Options{Config: config.Config{Model: "x"}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			systems = append(systems, req.System)
			if len(systems) == 1 {
				return ai.EmitMessage(ctx, &ai.AssistantMessage{
					Role:       ai.RoleAssistant,
					StopReason: ai.StopToolUse,
					Content: []*ai.Content{{
						Type: ai.KindToolCall, ToolID: "1", ToolName: "streamy",
						Arguments: map[string]any{},
					}},
				}), nil
			}
			return textReply("done")(ctx, req, opts)
		},
	}
	e.bindExtensionHooks()
	wireQueues(e)
	e.SetKick(func(string, []ai.ImageContent) {
		_ = e.RunPrompt(context.Background(), nil, "", nil).Collect()
	})
	_ = e.HandleHostCall(h, "sendMessage", map[string]any{
		"customType": "task-done", "content": "background task finished",
		"display": true, "triggerTurn": true,
	})
	if len(systems) != 2 || !strings.Contains(systems[0], "KEEP-ME") || systems[1] != systems[0] {
		t.Fatalf("systems = %q", systems)
	}
	ev := readMark(t, mark)[0]
	if ev["messageType"] != "custom" {
		t.Fatalf("event = %v", ev)
	}
}

func TestBeforeAgentStartHookMessageBothRuns(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	h := spawnAgentStartExt(t, "hookmsg", mark)
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	var seen []string
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			seen = nil
			for _, m := range req.Messages {
				if m.Role == ai.RoleUser {
					seen = append(seen, m.Content)
				}
			}
			return textReply("ok")(ctx, req, opts)
		},
	}
	e.bindExtensionHooks()
	wireQueues(e)
	_ = e.RunPrompt(context.Background(), nil, "hello", nil).Collect()
	if strings.Join(seen, "|") != "hello|from-hook" {
		t.Fatalf("user run = %v", seen)
	}
	hist := append([]ai.Message(nil), e.History()...)
	e.SetKick(func(string, []ai.ImageContent) {
		_ = e.RunPrompt(context.Background(), hist, "", nil).Collect()
	})
	_ = e.HandleHostCall(h, "sendMessage", map[string]any{
		"customType": "task-done", "content": "background task finished",
		"display": true, "triggerTurn": true,
	})
	joined := strings.Join(seen, "|")
	if !strings.Contains(joined, "background task finished") || strings.Count(joined, "from-hook") < 2 {
		t.Fatalf("custom run = %v", seen)
	}
	var details int
	for _, ent := range sess.Entries() {
		if ent.Type == "custom_message" && string(ent.Content) == `"from-hook"` && strings.Contains(string(ent.Details), `"k"`) {
			details++
		}
	}
	if details != 2 {
		t.Fatalf("hook messages with details = %d", details)
	}
}

func TestBeforeAgentStartAbortDuringHook(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	h := spawnAgentStartExt(t, "abort", mark)
	var providerCalls int
	var stop ai.StopReason
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Config: config.Config{Model: "x"}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			providerCalls++
			return textReply("should-not-run")(ctx, req, opts)
		},
	}
	e.bindExtensionHooks()
	wireQueues(e)
	e.SetKick(func(string, []ai.ImageContent) {
		events := e.RunPrompt(context.Background(), nil, "", nil).Collect()
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Assistant != nil {
				stop = events[i].Assistant.StopReason
				break
			}
		}
	})
	_ = e.HandleHostCall(h, "sendMessage", map[string]any{
		"customType": "task-done", "content": "background task finished",
		"display": true, "triggerTurn": true,
	})
	if providerCalls != 0 {
		t.Fatalf("provider calls = %d", providerCalls)
	}
	if stop != ai.StopAborted {
		t.Fatalf("stop = %q", stop)
	}
	if !e.IsIdle() {
		t.Fatal("expected idle after aborted run")
	}
	if rows := readMark(t, mark); len(rows) < 2 {
		t.Fatalf("hook log = %v", rows)
	}
}
