package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func textReply(s string) ai.StreamFn {
	return func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopStop,
			Content:    []*ai.Content{{Type: ai.KindText, Text: s}},
		}), nil
	}
}

func TestPersistTranscriptWritesOnlyNewMessages(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	e := &Engine{Opts: Options{Session: sess, Config: config.Config{Model: "x"}}}
	msgs := []agent.Msg{
		{Role: agent.RoleUser, Text: "hi"},
		{Role: agent.RoleAssistant, Assistant: &ai.AssistantMessage{Role: ai.RoleAssistant, Content: []*ai.Content{{Type: ai.KindText, Text: "yo"}}}},
	}
	e.PersistTranscript(msgs)
	e.PersistTranscript(msgs) // second persist of the same transcript must not duplicate
	got := session.RestoreAIMessages(sess.Entries())
	if len(got) != 2 {
		t.Fatalf("entries restored = %d, want 2 (no duplicates); %+v", len(got), got)
	}
}

func TestRPCSetModelAndPrompt(t *testing.T) {
	var calls int32
	e := &Engine{
		Stream: func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
			atomic.AddInt32(&calls, 1)
			return textReply("pong")(ctx, req, ai.Options{})
		},
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow

	in := strings.NewReader(`{"type":"set_model","provider":"openai","modelId":"gpt-4o"}
{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if e.Opts.Config.ResolvedModel() != "gpt-4o" {
		t.Fatalf("model = %s", e.Opts.Config.ResolvedModel())
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("prompt calls = %d", calls)
	}
	dec := json.NewDecoder(&out)
	var sawReady, sawAgent bool
	for {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			break
		}
		typ, _ := row["type"].(string)
		if typ == "ready" {
			sawReady = true
		}
		if strings.Contains(typ, "agent") {
			sawAgent = true
		}
	}
	if !sawReady {
		t.Fatalf("missing ready event in %s", out.String())
	}
	_ = sawAgent
}

func decodeRPCRows(t *testing.T, raw string) []map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var rows []map[string]any
	for {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			break
		}
		rows = append(rows, row)
	}
	return rows
}

func rpcRowsOfType(rows []map[string]any, typ string) []map[string]any {
	var out []map[string]any
	for _, r := range rows {
		if r["type"] == typ {
			out = append(out, r)
		}
	}
	return out
}

func TestRPCPromptEventStreamMatchesJSONEvent(t *testing.T) {
	e := &Engine{
		Stream:   textReply("pong"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow

	in := strings.NewReader(`{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	if len(rpcRowsOfType(rows, "agent_settled")) != 1 {
		t.Fatalf("missing agent_settled in %s", out.String())
	}
	ends := rpcRowsOfType(rows, "agent_end")
	if len(ends) != 1 {
		t.Fatalf("agent_end count = %d in %s", len(ends), out.String())
	}
	if ends[0]["willRetry"] != false {
		t.Fatalf("willRetry = %v", ends[0]["willRetry"])
	}
	updates := rpcRowsOfType(rows, "message_update")
	if len(updates) == 0 {
		t.Fatalf("no message_update in %s", out.String())
	}
	for _, u := range updates {
		if _, ok := u["text"]; ok {
			t.Fatalf("message_update still has shortcut text: %v", u)
		}
		if _, ok := u["message"]; ok {
			t.Fatalf("message_update still has cumulative message: %v", u)
		}
		if u["usage"] == nil || u["assistantMessageEvent"] == nil {
			t.Fatalf("message_update missing usage/assistantMessageEvent: %v", u)
		}
	}
	starts := rpcRowsOfType(rows, "message_start")
	var sawUser bool
	for _, s := range starts {
		msg, _ := s["message"].(map[string]any)
		if msg["role"] == "user" {
			sawUser = true
			if msg["content"] != "hi" {
				t.Fatalf("user message = %v", msg)
			}
		}
	}
	if !sawUser {
		t.Fatalf("missing user message_start in %s", out.String())
	}
}

func TestRPCPromptWhileStreamingRequiresBehavior(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	e := &Engine{
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return textReply("aborted")(ctx, req, opts)
			}
			return textReply("pong")(ctx, req, opts)
		},
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow

	pr, pw := io.Pipe()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- e.ServeRPC(context.Background(), pr, &out) }()

	enc := json.NewEncoder(pw)
	if err := enc.Encode(map[string]any{"type": "prompt", "message": "first", "id": "p1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider was not called")
	}
	if err := enc.Encode(map[string]any{"type": "prompt", "message": "second", "id": "p2"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := enc.Encode(map[string]any{"type": "quit"}); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeRPC did not return")
	}

	rows := decodeRPCRows(t, out.String())
	var sawReject bool
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "prompt" && r["id"] == "p2" {
			sawReject = true
			if r["success"] != false {
				t.Fatalf("second prompt success = %v, want false: %v", r["success"], r)
			}
			errStr, _ := r["error"].(string)
			if !strings.Contains(errStr, "streamingBehavior") {
				t.Fatalf("error = %q", errStr)
			}
		}
	}
	if !sawReject {
		t.Fatalf("missing rejected second prompt in %s", out.String())
	}
}

func TestRPCSteerEmitsQueueUpdate(t *testing.T) {
	e := &Engine{
		Stream:   textReply("pong"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow

	in := strings.NewReader(`{"type":"steer","message":"nudge"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	updates := rpcRowsOfType(rows, "queue_update")
	if len(updates) == 0 {
		t.Fatalf("missing queue_update in %s", out.String())
	}
	steering, _ := updates[0]["steering"].([]any)
	if len(steering) != 1 || steering[0] != "nudge" {
		t.Fatalf("steering = %v", updates[0]["steering"])
	}
}

func TestRPCThinkingLevelEmitsChanged(t *testing.T) {
	e := &Engine{
		Stream:   textReply("pong"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Thinking: "off"}},
	}
	in := strings.NewReader(`{"type":"set_thinking_level","level":"low"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	changed := rpcRowsOfType(rows, "thinking_level_changed")
	if len(changed) != 1 || changed[0]["level"] != "low" {
		t.Fatalf("thinking_level_changed = %v in %s", changed, out.String())
	}
}

func TestDrainQueueOneAtATimeVsAll(t *testing.T) {
	q := []ai.Message{
		{Role: ai.RoleUser, Content: "a"},
		{Role: ai.RoleUser, Content: "b"},
	}
	got := drainQueue(&q, "one-at-a-time")
	if len(got) != 1 || got[0].Content != "a" || len(q) != 1 || q[0].Content != "b" {
		t.Fatalf("one-at-a-time got=%+v remain=%+v", got, q)
	}
	q = []ai.Message{
		{Role: ai.RoleUser, Content: "a"},
		{Role: ai.RoleUser, Content: "b"},
	}
	got = drainQueue(&q, "all")
	if len(got) != 2 || q != nil && len(q) != 0 {
		t.Fatalf("all got=%+v remain=%+v", got, q)
	}
	got = drainQueue(&q, "")
	if got != nil {
		t.Fatalf("empty = %+v", got)
	}
}

func TestRPCGetTreeAndCycleThinking(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		Stream:   textReply("pong"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts: Options{
			Config:   config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Thinking: "off"},
			Session:  sess,
			Cwd:      cwd,
			AgentDir: dir,
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	e.AdoptSession(sess)

	in := strings.NewReader(`{"type":"get_tree"}
{"type":"get_entries"}
{"type":"cycle_thinking_level"}
{"type":"cycle_model"}
{"type":"get_fork_messages"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if e.Opts.Config.Thinking != "minimal" {
		t.Fatalf("thinking = %s want minimal", e.Opts.Config.Thinking)
	}
	s := out.String()
	if !strings.Contains(s, `"command":"get_tree"`) || !strings.Contains(s, `"success":true`) {
		t.Fatalf("missing get_tree response:\n%s", s)
	}
	if !strings.Contains(s, `"command":"get_entries"`) {
		t.Fatalf("missing get_entries:\n%s", s)
	}
	if !strings.Contains(s, `"command":"get_fork_messages"`) {
		t.Fatalf("missing get_fork_messages:\n%s", s)
	}
}

func TestRPCPromptAttachesImagesToUserMessage(t *testing.T) {
	e := &Engine{
		Stream:   textReply("pong"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow

	in := strings.NewReader(`{"type":"prompt","message":"look","images":[{"type":"image","data":"AAA","mimeType":"image/png"}]}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	var user map[string]any
	for _, s := range rpcRowsOfType(rows, "message_start") {
		msg, _ := s["message"].(map[string]any)
		if msg["role"] == "user" {
			user = msg
			break
		}
	}
	if user == nil {
		t.Fatalf("missing user message_start in %s", out.String())
	}
	blocks, ok := user["content"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("user content = %#v, want [text, image]", user["content"])
	}
	text, _ := blocks[0].(map[string]any)
	img, _ := blocks[1].(map[string]any)
	if text["type"] != "text" || text["text"] != "look" {
		t.Fatalf("text block = %#v", text)
	}
	if img["type"] != "image" || img["data"] != "AAA" || img["mimeType"] != "image/png" {
		t.Fatalf("image block = %#v", img)
	}
}

func TestRPCPromptPersistsImages(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	e := &Engine{
		Stream:   textReply("pong"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts: Options{
			Config:   config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
			Session:  sess,
			Cwd:      cwd,
			AgentDir: dir,
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	e.AdoptSession(sess)

	in := strings.NewReader(`{"type":"prompt","message":"look","images":[{"type":"image","data":"AAA","mimeType":"image/png"}]}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	msgs := session.RestoreAIMessages(sess.Entries())
	var user *ai.Message
	for i := range msgs {
		if msgs[i].Role == ai.RoleUser {
			user = &msgs[i]
			break
		}
	}
	if user == nil || user.Content != "look" || len(user.Images) != 1 || user.Images[0].Data != "AAA" {
		t.Fatalf("restored user = %+v from entries %+v", user, sess.Entries())
	}
}

func TestRPCSteerQueuesImages(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{SteeringMode: "one-at-a-time"}}}
	in := strings.NewReader(`{"type":"steer","message":"nudge","images":[{"type":"image","data":"BBB","mimeType":"image/jpeg"}]}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	got := e.drainSteer()
	if len(got) != 1 || got[0].Content != "nudge" || len(got[0].Images) != 1 || got[0].Images[0].Data != "BBB" {
		t.Fatalf("queued = %+v", got)
	}
}

func TestEnginePushSteerOneAtATime(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{SteeringMode: "one-at-a-time"}}}
	e.PushSteer("first")
	e.PushSteer("second")
	got := e.drainSteer()
	if len(got) != 1 || got[0].Content != "first" {
		t.Fatalf("%+v", got)
	}
	got = e.drainSteer()
	if len(got) != 1 || got[0].Content != "second" {
		t.Fatalf("%+v", got)
	}
}

func TestTakeQueuesClearsSteerAndFollow(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{}}}
	e.PushSteer("s1")
	e.PushFollow("f1")
	e.PushFollow("f2")
	steer, follow := e.TakeQueues()
	if len(steer) != 1 || steer[0] != "s1" {
		t.Fatalf("steer=%v", steer)
	}
	if len(follow) != 2 || follow[0] != "f1" || follow[1] != "f2" {
		t.Fatalf("follow=%v", follow)
	}
	if n := e.pendingCount(); n != 0 {
		t.Fatalf("pending=%d", n)
	}
}

func TestNoBuiltinToolsLeavesRegistryEmptyWithoutExtensions(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:            t.TempDir(),
		AgentDir:       t.TempDir(),
		NoBuiltinTools: true,
		NoExtensions:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if n := len(e.Tools.List()); n != 0 {
		t.Fatalf("tools=%d, want 0 builtins", n)
	}
}

func TestNoToolsSkipsCLIExtensions(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:           t.TempDir(),
		AgentDir:      t.TempDir(),
		NoTools:       true,
		CLIExtensions: []string{"/bin/true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if len(e.Hosts) != 0 {
		t.Fatalf("hosts=%d, --no-tools should not spawn extensions", len(e.Hosts))
	}
	if n := len(e.Tools.List()); n != 0 {
		t.Fatalf("tools=%d", n)
	}
}

func TestDefaultLoadsBuiltinTools(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	got := toolNames(e)
	want := []string{"read", "write", "edit", "bash"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tools=%v, want default read/write/edit/bash (got %v)", got, want)
	}
}

func TestProjectSettingsDefaultToolsWhenTrusted(t *testing.T) {
	ctx := context.Background()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "settings.json"), []byte(`{"defaultTools":["grep"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	user, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(ctx, Options{
		Cwd:            cwd,
		AgentDir:       t.TempDir(),
		NoExtensions:   true,
		ProjectTrusted: true,
		Config:         config.ApplyProject(user, cwd, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if strings.Join(toolNames(e), ",") != "grep" {
		t.Fatalf("trusted project defaultTools = %v", toolNames(e))
	}
}

func TestDefaultToolsSetting(t *testing.T) {
	ctx := context.Background()
	only := []string{"grep", "find"}
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		Config:       config.Config{DefaultTools: &only},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	got := toolNames(e)
	if strings.Join(got, ",") != "grep,find" {
		t.Fatalf("tools=%v", got)
	}
}

func TestToolsFlagOverridesDefaultTools(t *testing.T) {
	ctx := context.Background()
	only := []string{"grep"}
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		Config:       config.Config{DefaultTools: &only},
		ToolAllow:    []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	got := toolNames(e)
	if strings.Join(got, ",") != "read" {
		t.Fatalf("tools=%v", got)
	}
}

func TestEmptyDefaultToolsDisablesBuiltins(t *testing.T) {
	ctx := context.Background()
	none := []string{}
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		Config:       config.Config{DefaultTools: &none},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if n := len(e.Tools.List()); n != 0 {
		t.Fatalf("tools=%d, empty defaultTools should disable builtins", n)
	}
}

func toolNames(e *Engine) []string {
	var names []string
	for _, t := range e.Tools.List() {
		names = append(names, t.Name())
	}
	return names
}

func TestNewPicksAuthenticatedOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	e, err := New(context.Background(), Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		Config:       config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", e.Provider)
	}
	if e.Opts.Config.ResolvedModel() == "" {
		t.Fatal("expected openai default model")
	}
}

func TestNewHonorsCLIProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "")
	e, err := New(context.Background(), Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		CLIProvider:  "anthropic",
		CLIModel:     "claude-haiku-4",
		Config:       config.Config{Provider: "anthropic", Model: "claude-haiku-4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.Provider != "anthropic" || e.Opts.Config.ResolvedModel() != "claude-haiku-4" {
		t.Fatalf("provider=%s model=%s", e.Provider, e.Opts.Config.ResolvedModel())
	}
}

func TestApplyModelDoesNotOverwriteSavedDefault(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{
		Provider: "anthropic", Model: "claude-sonnet-4",
		DefaultProvider: "anthropic", DefaultModel: "claude-sonnet-4",
	}}}
	e.ApplyModel("openai", "gpt-4o", "")
	if e.Opts.Config.ResolvedModel() != "gpt-4o" {
		t.Fatalf("session model = %s", e.Opts.Config.ResolvedModel())
	}
	if e.Opts.Config.DefaultModel != "claude-sonnet-4" {
		t.Fatalf("default should stay, got %s", e.Opts.Config.DefaultModel)
	}
}

func TestPersistModelWritesSettings(t *testing.T) {
	dir := t.TempDir()
	e := &Engine{Opts: Options{
		AgentDir: dir,
		Config: config.Config{
			Provider: "anthropic", Model: "claude-sonnet-4",
			DefaultProvider: "anthropic", DefaultModel: "claude-sonnet-4",
			Theme: "default",
		},
	}}
	if err := e.PersistModel("openai", "gpt-4o", ""); err != nil {
		t.Fatal(err)
	}
	if e.Opts.Config.DefaultModel != "gpt-4o" {
		t.Fatalf("default = %s", e.Opts.Config.DefaultModel)
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefaultProvider != "openai" || loaded.DefaultModel != "gpt-4o" {
		t.Fatalf("saved %s/%s", loaded.DefaultProvider, loaded.DefaultModel)
	}
}

func TestHistoryFollowsLeafNotSiblings(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "main"})
	a, _ := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "side"})
	_, _ = sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "side-ok"})
	if _, err := sess.Navigate(a.ID, session.NavigateOpts{}); err != nil {
		t.Fatal(err)
	}
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "alt"})
	e := &Engine{Opts: Options{Session: sess}}
	var texts []string
	for _, m := range e.History() {
		texts = append(texts, m.Content)
	}
	joined := strings.Join(texts, ",")
	if strings.Contains(joined, "side") {
		t.Fatalf("abandoned leaked: %s", joined)
	}
	if !strings.Contains(joined, "alt") || !strings.Contains(joined, "main") {
		t.Fatalf("history = %s", joined)
	}
}

func TestRPCNavigateTree(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	a, _ := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})
	u2, _ := sess.AppendMessage("user", map[string]any{"role": "user", "content": "later"})
	_, _ = sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"})
	e := &Engine{
		Stream:   textReply("pong"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}, Session: sess, Cwd: cwd, AgentDir: dir},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	in := strings.NewReader(`{"type":"navigate_tree","targetId":"` + u2.ID + `"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"command":"navigate_tree"`) || !strings.Contains(out.String(), `"success":true`) {
		t.Fatalf("rpc:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"editorText":"later"`) {
		t.Fatalf("missing editorText:\n%s", out.String())
	}
	if sess.LeafID() != a.ID {
		t.Fatalf("leaf = %s want %s", sess.LeafID(), a.ID)
	}
}
