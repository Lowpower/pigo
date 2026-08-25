package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
