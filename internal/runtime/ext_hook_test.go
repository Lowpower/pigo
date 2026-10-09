package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/telemetry"
	"github.com/Lowpower/pigo/internal/tools"
)

func newMockHostEngine(h *ext.Host) *Engine {
	return &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Config: config.Config{Model: "x", Provider: "mock"}},
	}
}

func TestRuntimeHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_RUNTIME_EXT") == "" {
		return
	}
	switch os.Getenv("PIGO_RUNTIME_EXT") {
	case "handled":
		_ = ext.Serve(ext.Handler{
			Name: "handled-ext",
			Commands: []ext.CommandDef{{
				Name:        "ping",
				Description: "consumed by the extension",
			}},
		})
	case "compact":
		_ = ext.Serve(ext.Handler{
			Name:   "compact-ext",
			Events: []string{"session_before_compact"},
			OnEvent: func(string, map[string]any) map[string]any {
				return map[string]any{"compaction": "HOOK SUMMARY"}
			},
		})
	case "block":
		_ = ext.Serve(ext.Handler{
			Name:   "block-ext",
			Events: []string{"tool_call", "input"},
			OnEvent: func(event string, _ map[string]any) map[string]any {
				if event == "tool_call" {
					return map[string]any{"block": true, "reason": "nope"}
				}
				if event == "input" {
					return map[string]any{"action": "transform", "text": "transformed"}
				}
				return nil
			},
		})
	case "flag":
		_ = ext.Serve(ext.Handler{
			Name: "flag-ext",
			Flags: []ext.FlagDef{{
				Name: "plan", Type: "boolean", Description: "plan",
			}},
		})
	case "stream":
		_ = ext.Serve(ext.Handler{
			Name: "stream-ext",
			Providers: []ext.ProviderDef{{
				ID: "capdemo",
				Args: map[string]any{
					"name":   "capdemo",
					"stream": true,
					"models": []any{map[string]any{"id": "demo"}},
				},
			}},
			OnStream: func(_ map[string]any, emit func(event string, payload map[string]any), _ <-chan struct{}) {
				emit("start", map[string]any{})
				emit("text_start", map[string]any{"contentIndex": 0.0})
				emit("text_delta", map[string]any{"contentIndex": 0.0, "delta": "hello from capdemo"})
				emit("text_end", map[string]any{"contentIndex": 0.0, "content": "hello from capdemo"})
				emit("done", map[string]any{
					"message": map[string]any{
						"role": "assistant", "stopReason": "stop",
						"content": []any{map[string]any{"type": "text", "text": "hello from capdemo"}},
					},
				})
			},
		})
	case "context":
		_ = ext.Serve(ext.Handler{
			Name:   "context-ext",
			Events: []string{"context"},
			OnEvent: func(string, map[string]any) map[string]any {
				return map[string]any{
					"messages": []any{map[string]any{"role": "user", "content": "from-ext"}},
				}
			},
		})
	case "context-hooks":
		_ = ext.Serve(ext.Handler{
			Name:   "context-hooks",
			Events: []string{"context", "context_with_system"},
			OnEvent: func(event string, payload map[string]any) map[string]any {
				if p := os.Getenv("PIGO_EXT_LOG"); p != "" {
					f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
					if err == nil {
						raw, _ := json.Marshal(payload["messages"])
						_, _ = fmt.Fprintf(f, "%s %s\n", event, raw)
						_ = f.Close()
					}
				}
				msgs, _ := payload["messages"].([]any)
				switch os.Getenv("PIGO_CONTEXT_MODE") {
				case "drop":
					if event == "context_with_system" {
						return map[string]any{"messages": []any{map[string]any{"role": "user", "content": "no-system"}}}
					}
				case "rewrite":
					if event == "context_with_system" {
						return map[string]any{"messages": []any{
							map[string]any{
								"role": "system", "content": "CHANGED",
								"tools": []any{map[string]any{"name": "other", "description": "other"}},
							},
							map[string]any{"role": "user", "content": "kept"},
						}}
					}
				}
				if event == "context" && len(msgs) > 0 {
					return map[string]any{"messages": []any{msgs[len(msgs)-1]}}
				}
				return nil
			},
		})
	case "resources":
		_ = ext.Serve(ext.Handler{
			Name:   "res-ext",
			Events: []string{"resources_discover"},
			OnEvent: func(string, map[string]any) map[string]any {
				return map[string]any{"skillPaths": []string{os.Getenv("PIGO_EXT_SKILLS")}}
			},
		})
	case "headers":
		_ = ext.Serve(ext.Handler{
			Name:   "hdr-ext",
			Events: []string{"before_provider_headers"},
			OnEvent: func(string, map[string]any) map[string]any {
				return map[string]any{"headers": map[string]any{"X-Pigo-Test": "1"}}
			},
		})
	case "provider-stream":
		_ = ext.Serve(ext.Handler{
			Name:   "provider-stream-ext",
			Events: []string{"provider_stream_event", "message_update"},
			OnEvent: func(event string, payload map[string]any) map[string]any {
				if p := os.Getenv("PIGO_EXT_LOG"); p != "" {
					line := event
					raw, _ := json.Marshal(payload)
					switch event {
					case "provider_stream_event":
						data, _ := payload["data"].(map[string]any)
						line = fmt.Sprintf("provider_stream_event %v %v %v %v", payload["provider"], payload["api"], payload["model"], data["type"])
						if note, _ := data["vendorNote"].(string); note != "" {
							line += " " + note
						}
					case "message_update":
						ame, _ := payload["assistantMessageEvent"].(map[string]any)
						line = fmt.Sprintf("message_update %v", ame["type"])
						if strings.Contains(string(raw), "vendorNote") {
							line += " HAS_VENDOR"
						}
					}
					if strings.Contains(string(raw), "sk-provider-secret") {
						line += " LEAKED"
					}
					f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
					if err == nil {
						_, _ = fmt.Fprintln(f, line)
						_ = f.Close()
					}
				}
				return nil
			},
		})
	case "messages":
		_ = ext.Serve(ext.Handler{
			Name: "msg-ext",
			Events: []string{
				"message_start", "message_update", "message_end", "tool_execution_update",
			},
			OnEvent: func(event string, payload map[string]any) map[string]any {
				if p := os.Getenv("PIGO_EXT_LOG"); p != "" {
					f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
					if err == nil {
						_, _ = fmt.Fprintln(f, event)
						_ = f.Close()
					}
				}
				if event != "message_end" {
					return nil
				}
				msg, _ := payload["message"].(map[string]any)
				if msg == nil {
					return nil
				}
				msg["errorMessage"] = "from-ext"
				return map[string]any{"message": msg}
			},
		})
	case "forcesys":
		_ = ext.Serve(ext.Handler{
			Name:   "force-ext",
			Events: []string{"before_agent_start"},
			OnEvent: func(string, map[string]any) map[string]any {
				if os.Getenv("PIGO_FORCE_SYS") == "turn" {
					return map[string]any{"systemPrompt": "TURN-ONLY"}
				}
				return map[string]any{"forceSystemPrompt": "LEAD"}
			},
		})
	case "userbash":
		_ = ext.Serve(ext.Handler{
			Name:   "userbash-ext",
			Events: []string{"user_bash"},
			OnEvent: func(string, map[string]any) map[string]any {
				switch os.Getenv("PIGO_USER_BASH") {
				case "result":
					return map[string]any{"result": map[string]any{
						"output": "from-ext", "cancelled": false, "truncated": false, "exitCode": 0.0,
					}}
				case "invalid":
					return map[string]any{"block": true}
				default:
					return nil
				}
			},
		})
	case "boundary":
		_ = ext.Serve(ext.Handler{
			Name:   "boundary-ext",
			Events: []string{"turn_end", "agent_before_settle"},
			OnEvent: func(event string, payload map[string]any) map[string]any {
				mode := os.Getenv("PIGO_BOUNDARY")
				mark := os.Getenv("PIGO_BOUNDARY_MARK")
				seen := false
				if mark != "" {
					if _, err := os.Stat(mark); err == nil {
						seen = true
					}
				}
				if mode == "turn" && event == "turn_end" {
					if seen {
						return nil
					}
					if mark != "" {
						_ = os.WriteFile(mark, []byte("1"), 0o644)
					}
					return map[string]any{
						"entries": []any{map[string]any{
							"type":        "context_edit",
							"targetId":    payload["messageEntryId"],
							"replacement": nil,
						}},
						"continue": true,
					}
				}
				if mode == "settle" && event == "agent_before_settle" {
					if seen {
						return nil
					}
					if mark != "" {
						_ = os.WriteFile(mark, []byte("1"), 0o644)
					}
					return map[string]any{
						"entries": []any{map[string]any{
							"type":       "custom_message",
							"customType": "review",
							"content":    "review please",
							"display":    false,
						}},
						"continue": true,
					}
				}
				if mode == "bad" && event == "turn_end" {
					return map[string]any{
						"entries":  []any{map[string]any{"type": "nope"}},
						"continue": true,
					}
				}
				if mode == "error" && event == "turn_end" {
					return map[string]any{"continue": true}
				}
				return nil
			},
		})
	case "span":
		_ = ext.Serve(ext.Handler{
			Name:   "span-ext",
			Events: []string{"telemetry_span"},
			OnEvent: func(event string, payload map[string]any) map[string]any {
				if p := os.Getenv("PIGO_EXT_LOG"); p != "" {
					f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
					if err == nil {
						name, _ := payload["name"].(string)
						_, _ = fmt.Fprintln(f, event, name)
						_ = f.Close()
					}
				}
				return nil
			},
		})
	}
	os.Exit(0)
}

func spawnRuntimeExt(t *testing.T, kind string, unknown []ext.UnknownFlag, extraEnv ...string) *ext.Host {
	t.Helper()
	env := append([]string{"PIGO_RUNTIME_EXT=" + kind}, extraEnv...)
	h, err := ext.Spawn(context.Background(), "runtime-ext",
		[]string{os.Args[0], "-test.run=^TestRuntimeHelperProcess$"},
		ext.Options{Env: env, UnknownFlags: unknown})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func TestExecutorBlocksToolCall(t *testing.T) {
	h := spawnRuntimeExt(t, "block", nil)
	e := &Engine{
		Hosts: []*ext.Host{h},
		Tools: tools.Default(),
		Opts:  Options{Config: config.Config{Model: "x"}},
	}
	got, isErr := e.Executor().Execute(context.Background(), agent.ToolCall{ID: "1", Name: "read", Args: map[string]any{"path": "a"}})
	if !isErr || got != "nope" {
		t.Fatalf("got %q isErr=%v", got, isErr)
	}
}

func TestRunPromptTransformsInput(t *testing.T) {
	h := spawnRuntimeExt(t, "block", nil)
	var seen string
	e := &Engine{
		Hosts: []*ext.Host{h},
		Stream: func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
			if len(req.Messages) > 0 {
				seen = req.Messages[len(req.Messages)-1].Content
			}
			return textReply("ok")(ctx, req, ai.Options{})
		},
		Opts: Options{Config: config.Config{Model: "x", Provider: "mock"}},
	}
	wireQueues(e)
	st := e.RunPrompt(context.Background(), nil, "original", nil)
	_ = st.Collect()
	if seen != "transformed" {
		t.Fatalf("prompt = %q, want transformed", seen)
	}
}

func TestUnclaimedFlags(t *testing.T) {
	h := spawnRuntimeExt(t, "flag", []ext.UnknownFlag{
		{Name: "plan", Present: true},
		{Name: "orphan", Present: true},
	})
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{UnknownFlags: []ext.UnknownFlag{
		{Name: "plan", Present: true},
		{Name: "orphan", Present: true},
	}}}
	left := e.UnclaimedFlags()
	if len(left) != 1 || left[0].Name != "orphan" {
		t.Fatalf("leftover=%+v", left)
	}
}

func TestBindExtensionStream(t *testing.T) {
	h := spawnRuntimeExt(t, "stream", nil)
	e := &Engine{
		Opts:     Options{AgentDir: t.TempDir(), Config: config.Config{Provider: "capdemo", Model: "demo"}},
		Provider: "capdemo",
		Hosts:    []*ext.Host{h},
	}
	e.applyProviders()
	t.Cleanup(func() { e.dropAllProviders() })
	fn := e.bindStream("capdemo")
	if fn == nil {
		t.Fatal("missing stream")
	}
	es, err := fn(context.Background(), ai.Context{}, ai.Options{Model: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	_, msg := es.Collect()
	if msg == nil || msg.Text() != "hello from capdemo" {
		t.Fatalf("got %+v", msg)
	}
}

func TestContextHookRestoresPromptAndTools(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "hooks.log")
	h := spawnRuntimeExt(t, "context-hooks", nil, "PIGO_CONTEXT_MODE=restore", "PIGO_EXT_LOG="+logPath)
	var seen ai.Context
	inner := func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
		seen = req
		return textReply("ok")(ctx, req, ai.Options{})
	}
	e := newMockHostEngine(h)
	fn := e.gatedStream(inner)
	_, err := fn(context.Background(), ai.Context{
		System: "PROMPT",
		Tools:  []ai.Tool{{Name: "read", Description: "read a file", Parameters: map[string]any{"type": "object"}}},
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: "mid-system"},
			{Role: ai.RoleUser, Content: "keep"},
			{Role: ai.RoleUser, Content: "tail"},
		},
	}, ai.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if seen.System != "PROMPT" {
		t.Fatalf("system = %q", seen.System)
	}
	if len(seen.Tools) != 1 || seen.Tools[0].Name != "read" {
		t.Fatalf("tools = %+v", seen.Tools)
	}
	if len(seen.Messages) != 1 || seen.Messages[0].Content != "tail" || seen.Messages[0].Role == ai.RoleSystem {
		t.Fatalf("messages = %+v", seen.Messages)
	}
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "context ") || strings.Contains(strings.Split(text, "\n")[0], "mid-system") || strings.Contains(strings.Split(text, "\n")[0], `"role":"system"`) {
		t.Fatalf("context saw system messages:\n%s", text)
	}
	if !strings.Contains(text, "context_with_system ") || !strings.Contains(text, "PROMPT") {
		t.Fatalf("context_with_system log:\n%s", text)
	}
}

func TestContextWithSystemSendsResultVerbatim(t *testing.T) {
	h := spawnRuntimeExt(t, "context-hooks", nil, "PIGO_CONTEXT_MODE=rewrite")
	var seen ai.Context
	var calls int
	inner := func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
		calls++
		seen = req
		return textReply("ok")(ctx, req, ai.Options{})
	}
	e := newMockHostEngine(h)
	_, err := e.gatedStream(inner)(context.Background(), ai.Context{
		System:   "PROMPT",
		Tools:    []ai.Tool{{Name: "read", Description: "read"}},
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "tail"}},
	}, ai.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || seen.System != "CHANGED" {
		t.Fatalf("calls=%d system=%q", calls, seen.System)
	}
	if len(seen.Tools) != 1 || seen.Tools[0].Name != "other" {
		t.Fatalf("tools=%+v", seen.Tools)
	}
	if len(seen.Messages) != 1 || seen.Messages[0].Content != "kept" {
		t.Fatalf("messages=%+v", seen.Messages)
	}
}

func TestContextWithSystemRejectsMissingHead(t *testing.T) {
	h := spawnRuntimeExt(t, "context-hooks", nil, "PIGO_CONTEXT_MODE=drop")
	calls := 0
	inner := func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
		calls++
		return textReply("ok")(ctx, req, ai.Options{})
	}
	e := newMockHostEngine(h)
	_, err := e.gatedStream(inner)(context.Background(), ai.Context{
		System:   "PROMPT",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "tail"}},
	}, ai.Options{})
	if err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestGatedStreamContextReplacesMessages(t *testing.T) {
	h := spawnRuntimeExt(t, "context", nil)
	var seen []ai.Message
	inner := func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
		seen = append([]ai.Message(nil), req.Messages...)
		return textReply("ok")(ctx, req, ai.Options{})
	}
	e := newMockHostEngine(h)
	wireQueues(e)
	e.Stream = e.gatedStream(inner)
	_ = e.RunPrompt(context.Background(), nil, "original", nil).Collect()
	if len(seen) != 1 || seen[0].Content != "from-ext" {
		t.Fatalf("messages=%+v", seen)
	}
}

func TestResourcesDiscoverInjectsSkills(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: ext-skill\ndescription: From extension\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	h := spawnRuntimeExt(t, "resources", nil, "PIGO_EXT_SKILLS="+dir)
	e := &Engine{
		Hosts: []*ext.Host{h},
		Tools: tools.Default(),
		Opts:  Options{Cwd: t.TempDir()},
	}
	e.extendResourcesFromExtensions(context.Background(), "startup")
	e.rebuildSystemPrompt()
	if !strings.Contains(e.System, "ext-skill") {
		t.Fatalf("system prompt missing injected skill:\n%s", e.System)
	}
}

func TestProviderStreamEventNotInstalledWithoutSubscriber(t *testing.T) {
	h := spawnRuntimeExt(t, "messages", nil)
	var installed bool
	e := newMockHostEngine(h)
	wireQueues(e)
	e.Stream = e.gatedStream(func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		installed = opts.OnProviderStreamEvent != nil
		return textReply("ok")(ctx, req, ai.Options{})
	})
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if installed {
		t.Fatal("OnProviderStreamEvent installed without a subscriber")
	}
}

func TestProviderStreamEventBeforeMessageUpdate(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "events.log")
	h := spawnRuntimeExt(t, "provider-stream", nil, "PIGO_EXT_LOG="+logPath)
	const fixture = "" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"},\"vendorNote\":\"raw-delta\"}\n\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, fixture)
	}))
	t.Cleanup(srv.Close)
	client := &ai.AnthropicClient{BaseURL: srv.URL, APIKey: "sk-provider-secret", HTTPClient: srv.Client()}
	e := &Engine{
		Hosts:    []*ext.Host{h},
		Provider: "anthropic",
		Opts:     Options{Config: config.Config{Model: "claude-test", Provider: "anthropic"}},
	}
	wireQueues(e)
	e.Stream = e.gatedStream(client.StreamFn())
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()

	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	rawAt, normAt := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "LEAKED") {
			t.Fatalf("payload included a secret that was not in the event: %s", line)
		}
		if strings.HasPrefix(line, "provider_stream_event anthropic anthropic-messages claude-test content_block_delta raw-delta") {
			rawAt = i
		}
		if line == "message_update text_delta" {
			normAt = i
		}
		if strings.HasPrefix(line, "message_update") && strings.Contains(line, "HAS_VENDOR") {
			t.Fatalf("message_update included the raw vendor field: %s", line)
		}
	}
	if rawAt < 0 || normAt < 0 || rawAt > normAt {
		t.Fatalf("raw event must precede text_delta message_update:\n%s", body)
	}
}

func TestGatedStreamBeforeProviderHeaders(t *testing.T) {
	h := spawnRuntimeExt(t, "headers", nil)
	var got map[string]string
	inner := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		got = opts.ExtraHeaders
		return textReply("ok")(ctx, req, ai.Options{})
	}
	e := newMockHostEngine(h)
	wireQueues(e)
	e.Stream = e.gatedStream(inner)
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if got["X-Pigo-Test"] != "1" {
		t.Fatalf("headers=%v", got)
	}
}

type streamyTool struct{}

func (streamyTool) Name() string        { return "streamy" }
func (streamyTool) Description() string { return "stream" }
func (streamyTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (streamyTool) Execute(ctx context.Context, _ map[string]any) (string, bool) {
	if fn := tools.OutputUpdate(ctx); fn != nil {
		fn("partial-out")
	}
	return "done", false
}

func TestMessageAndToolUpdateEvents(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "events.log")
	h := spawnRuntimeExt(t, "messages", nil, "PIGO_EXT_LOG="+logPath)
	var n int32
	e := &Engine{
		Hosts: []*ext.Host{h},
		Tools: tools.NewRegistry(streamyTool{}),
		Opts:  Options{Config: config.Config{Model: "x", Provider: "mock"}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			if atomic.AddInt32(&n, 1) == 1 {
				return ai.EmitMessage(ctx, &ai.AssistantMessage{
					Role:       ai.RoleAssistant,
					StopReason: ai.StopToolUse,
					Content: []*ai.Content{{
						Type: ai.KindToolCall, ToolID: "1", ToolName: "streamy",
						Arguments: map[string]any{},
					}},
				}), nil
			}
			return textReply("ok")(ctx, req, opts)
		},
	}
	wireQueues(e)
	events := e.RunPrompt(context.Background(), nil, "go", nil).Collect()
	var end *ai.AssistantMessage
	for _, ev := range events {
		if ev.Type == agent.EventMessageEnd && ev.Assistant != nil && ev.Assistant.StopReason == ai.StopStop {
			end = ev.Assistant
		}
	}
	if end == nil || end.ErrorMessage != "from-ext" {
		t.Fatalf("message_end replacement = %+v", end)
	}
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, want := range []string{"message_start", "message_update", "message_end", "tool_execution_update"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %s:\n%s", want, got)
		}
	}
}

func TestSpanEventOnlyToSubscribedHosts(t *testing.T) {
	telemetry.Reset()
	t.Cleanup(telemetry.Reset)
	logPath := filepath.Join(t.TempDir(), "span.log")
	sub := spawnRuntimeExt(t, "span", nil, "PIGO_EXT_LOG="+logPath)
	other := spawnRuntimeExt(t, "flag", nil)
	if !sub.Subscribed(telemetry.EventSpan) {
		t.Fatal("span-ext should subscribe")
	}
	if other.Subscribed(telemetry.EventSpan) {
		t.Fatal("flag-ext should not subscribe")
	}
	e := &Engine{
		Hosts: []*ext.Host{sub, other},
		Opts:  Options{Config: config.Config{Model: "x"}},
	}
	e.wireTelemetry()
	_, span := telemetry.Start(context.Background(), "pigo.turn")
	span.End()
	_ = telemetry.Shutdown(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(logPath)
		got = string(b)
		if strings.Contains(got, "telemetry_span") && strings.Contains(got, "pigo.turn") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("subscribed host log = %q", got)
}

func TestSpanEventDeniedSkipsExtension(t *testing.T) {
	telemetry.Reset()
	t.Cleanup(telemetry.Reset)
	t.Setenv("PIGO_TELEMETRY", "0")
	logPath := filepath.Join(t.TempDir(), "span.log")
	h := spawnRuntimeExt(t, "span", nil, "PIGO_EXT_LOG="+logPath)
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Config: config.Config{Model: "x"}}}
	e.wireTelemetry()
	_, span := telemetry.Start(context.Background(), "pigo.turn")
	span.End()
	_ = telemetry.Shutdown(context.Background())
	time.Sleep(50 * time.Millisecond)
	b, err := os.ReadFile(logPath)
	if err == nil && strings.Contains(string(b), "telemetry_span") {
		t.Fatalf("denied export still sent: %s", b)
	}
}

func TestRunUserBashExtensionModes(t *testing.T) {
	tests := []struct {
		mode, command string
		check         func(BashResult) bool
	}{
		{"result", "echo should-not-run", func(r BashResult) bool { return r.Error == "" && r.Output == "from-ext" }},
		{"invalid", "echo should-not-run", func(r BashResult) bool {
			return r.Error != "" && !strings.Contains(r.Output, "should-not-run")
		}},
		{"empty", "printf pigo-local", func(r BashResult) bool { return r.Error == "" && strings.Contains(r.Output, "pigo-local") }},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH="+tt.mode)
			e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
			res := e.RunUserBash(context.Background(), tt.command, false, nil)
			if !tt.check(res) {
				t.Fatalf("%+v", res)
			}
		})
	}
}
