package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

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
