package runtime

import (
	"context"
	"fmt"
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

func TestRuntimeHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_RUNTIME_EXT") == "" {
		return
	}
	switch os.Getenv("PIGO_RUNTIME_EXT") {
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
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
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

func TestGatedStreamContextReplacesMessages(t *testing.T) {
	h := spawnRuntimeExt(t, "context", nil)
	var seen []ai.Message
	inner := func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
		seen = append([]ai.Message(nil), req.Messages...)
		return textReply("ok")(ctx, req, ai.Options{})
	}
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Config: config.Config{Model: "x", Provider: "mock"}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
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

func TestGatedStreamBeforeProviderHeaders(t *testing.T) {
	h := spawnRuntimeExt(t, "headers", nil)
	var got map[string]string
	inner := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		got = opts.ExtraHeaders
		return textReply("ok")(ctx, req, ai.Options{})
	}
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Config: config.Config{Model: "x", Provider: "mock"}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
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
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
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

func TestUserBashResultReplacesLocal(t *testing.T) {
	h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH=result")
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
	res := e.RunUserBash(context.Background(), "echo should-not-run", false, nil)
	if res.Error != "" || res.Output != "from-ext" {
		t.Fatalf("%+v", res)
	}
}

func TestUserBashInvalidFailsClosed(t *testing.T) {
	h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH=invalid")
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
	res := e.RunUserBash(context.Background(), "echo should-not-run", false, nil)
	if res.Error == "" || strings.Contains(res.Output, "should-not-run") {
		t.Fatalf("%+v", res)
	}
}

func TestUserBashEmptyRunsLocal(t *testing.T) {
	h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH=empty")
	dir := t.TempDir()
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: dir}}
	res := e.RunUserBash(context.Background(), "printf pigo-local", false, nil)
	if res.Error != "" || !strings.Contains(res.Output, "pigo-local") {
		t.Fatalf("%+v", res)
	}
}
