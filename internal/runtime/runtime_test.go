package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
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

func TestRunPromptPersistsUserBeforeProvider(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	var sawUserAtCall bool
	e := &Engine{
		Opts: Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
		Stream: func(context.Context, ai.Context, ai.Options) (*ai.EventStream, error) {
			if n := countSessionRole(t, sess.File(), "user"); n != 1 {
				t.Errorf("user entries when provider is called = %d, want 1", n)
			} else {
				sawUserAtCall = true
			}
			return nil, fmt.Errorf("provider down")
		},
	}
	wireQueues(e)
	events := e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if !sawUserAtCall {
		t.Fatal("provider was not called")
	}
	var last []agent.Msg
	for _, ev := range events {
		if ev.Type == agent.EventAgentEnd {
			last = ev.Messages
		}
	}
	e.PersistTurn(last, 0)
	if n := countSessionRole(t, sess.File(), "user"); n != 1 {
		t.Fatalf("user entries after turn = %d, want 1", n)
	}
}

func countSessionRole(t *testing.T, path, role string) int {
	t.Helper()
	_, entries, err := session.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		var msg struct {
			Role string `json:"role"`
		}
		if len(e.Message) == 0 {
			continue
		}
		if err := json.Unmarshal(e.Message, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Role == role {
			n++
		}
	}
	return n
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
	wireQueues(e)

	out := serveRPC(t, e, `{"type":"set_model","provider":"openai","modelId":"gpt-4o"}
{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	if e.Opts.Config.ResolvedModel() != "gpt-4o" {
		t.Fatalf("model = %s", e.Opts.Config.ResolvedModel())
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("prompt calls = %d", calls)
	}
	dec := json.NewDecoder(strings.NewReader(out))
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
		t.Fatalf("missing ready event in %s", out)
	}
	_ = sawAgent
}

// newPongEngine returns an engine whose provider always answers "pong".
func newPongEngine(opts Options) *Engine {
	return &Engine{
		Stream:   textReply("pong"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     opts,
	}
}

// wireQueues connects the engine's steering and follow-up queues to the agent loop.
func wireQueues(e *Engine) {
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
}

// serveRPC feeds input to e.ServeRPC and returns everything written to the client.
func serveRPC(t *testing.T, e *Engine, input string) string {
	t.Helper()
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
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
	e := newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}})
	wireQueues(e)

	out := serveRPC(t, e, `{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	rows := decodeRPCRows(t, out)
	if len(rpcRowsOfType(rows, "agent_settled")) != 1 {
		t.Fatalf("missing agent_settled in %s", out)
	}
	ends := rpcRowsOfType(rows, "agent_end")
	if len(ends) != 1 {
		t.Fatalf("agent_end count = %d in %s", len(ends), out)
	}
	if ends[0]["willRetry"] != false {
		t.Fatalf("willRetry = %v", ends[0]["willRetry"])
	}
	updates := rpcRowsOfType(rows, "message_update")
	if len(updates) == 0 {
		t.Fatalf("no message_update in %s", out)
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
		t.Fatalf("missing user message_start in %s", out)
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
	wireQueues(e)

	c := startRPC(t, e)
	c.send(map[string]any{"type": "prompt", "message": "first", "id": "p1"})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider was not called")
	}
	c.send(map[string]any{"type": "prompt", "message": "second", "id": "p2"})
	close(release)
	out := c.close()

	rows := decodeRPCRows(t, out)
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
		t.Fatalf("missing rejected second prompt in %s", out)
	}
	reject := rpcResponse(rows, "prompt", "p2")
	if _, ok := reject["data"]; ok {
		t.Fatalf("rejected prompt has data: %v", reject)
	}
}

func TestRPCPromptDisposition(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	e := &Engine{
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			startedOnce.Do(func() { close(started) })
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
	wireQueues(e)

	c := startRPC(t, e)
	c.send(map[string]any{"type": "prompt", "message": "first", "id": "p1"})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider was not called")
	}
	c.send(map[string]any{"type": "steer", "message": "nudge-steer", "id": "s1"})
	waitQueueText(t, e, "nudge-steer", "")
	c.send(map[string]any{"type": "follow_up", "message": "nudge-follow", "id": "f1"})
	waitQueueText(t, e, "nudge-steer", "nudge-follow")
	c.send(map[string]any{
		"type": "prompt", "message": "via-steer", "id": "ps", "streamingBehavior": "steer",
	})
	waitQueueText(t, e, "via-steer", "nudge-follow")
	c.send(map[string]any{
		"type": "prompt", "message": "via-follow", "id": "pf", "streamingBehavior": "followUp",
	})
	waitQueueText(t, e, "via-steer", "via-follow")

	close(release)
	out := c.close()

	rows := decodeRPCRows(t, out)
	if got := responseDisposition(t, rows, "prompt", "p1"); got != "started" {
		t.Fatalf("idle prompt disposition = %q, want started", got)
	}
	if got := responseDisposition(t, rows, "steer", "s1"); got != "queued" {
		t.Fatalf("steer disposition = %q, want queued", got)
	}
	if got := responseDisposition(t, rows, "follow_up", "f1"); got != "queued" {
		t.Fatalf("follow_up disposition = %q, want queued", got)
	}
	if got := responseDisposition(t, rows, "prompt", "ps"); got != "queued" {
		t.Fatalf("streaming steer prompt disposition = %q, want queued", got)
	}
	if got := responseDisposition(t, rows, "prompt", "pf"); got != "queued" {
		t.Fatalf("streaming followUp prompt disposition = %q, want queued", got)
	}
}

func TestRPCHandledDisposition(t *testing.T) {
	h := spawnRuntimeExt(t, "handled", nil)
	var calls int32
	e := &Engine{
		Hosts: []*ext.Host{h},
		Stream: func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
			atomic.AddInt32(&calls, 1)
			return textReply("pong")(ctx, req, ai.Options{})
		},
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	e.rebuildCommands()
	wireQueues(e)

	out := serveRPC(t, e, `{"id":"p","type":"prompt","message":"/ping"}
{"id":"s","type":"steer","message":"/ping"}
{"id":"f","type":"follow_up","message":"/ping"}
{"type":"quit"}
`)
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("handled input started a turn: calls=%d", calls)
	}
	if n := e.pendingCount(); n != 0 {
		t.Fatalf("handled input queued: pending=%d", n)
	}
	rows := decodeRPCRows(t, out)
	for _, spec := range []struct{ command, id string }{
		{"prompt", "p"},
		{"steer", "s"},
		{"follow_up", "f"},
	} {
		if got := responseDisposition(t, rows, spec.command, spec.id); got != "handled" {
			t.Fatalf("%s disposition = %q, want handled", spec.command, got)
		}
	}
}

func rpcResponse(rows []map[string]any, command, id string) map[string]any {
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == command && r["id"] == id {
			return r
		}
	}
	return nil
}

func responseDisposition(t *testing.T, rows []map[string]any, command, id string) string {
	t.Helper()
	row := rpcResponse(rows, command, id)
	if row == nil {
		t.Fatalf("missing %s response id=%s", command, id)
	}
	if row["success"] != true {
		t.Fatalf("%s success = %v, want true: %v", command, row["success"], row)
	}
	data, _ := row["data"].(map[string]any)
	d, _ := data["disposition"].(string)
	return d
}

func waitQueueText(t *testing.T, e *Engine, steer, follow string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gotSteer, gotFollow := e.queueTexts()
		if (steer == "" || containsString(gotSteer, steer)) && (follow == "" || containsString(gotFollow, follow)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	gotSteer, gotFollow := e.queueTexts()
	t.Fatalf("queue steer=%v follow=%v, want %q / %q", gotSteer, gotFollow, steer, follow)
}

func TestRPCSteerEmitsQueueUpdate(t *testing.T) {
	e := newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}})
	wireQueues(e)

	out := serveRPC(t, e, `{"type":"steer","message":"nudge"}
{"type":"quit"}
`)
	rows := decodeRPCRows(t, out)
	updates := rpcRowsOfType(rows, "queue_update")
	if len(updates) == 0 {
		t.Fatalf("missing queue_update in %s", out)
	}
	steering, _ := updates[0]["steering"].([]any)
	if len(steering) != 1 || steering[0] != "nudge" {
		t.Fatalf("steering = %v", updates[0]["steering"])
	}
}

func TestRPCThinkingLevelEmitsChanged(t *testing.T) {
	e := newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Thinking: "off"}})
	out := serveRPC(t, e, `{"type":"set_thinking_level","level":"low"}
{"type":"quit"}
`)
	rows := decodeRPCRows(t, out)
	changed := rpcRowsOfType(rows, "thinking_level_changed")
	if len(changed) != 1 || changed[0]["level"] != "low" {
		t.Fatalf("thinking_level_changed = %v in %s", changed, out)
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
	e := newPongEngine(Options{
		Config:   config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Thinking: "off"},
		Session:  sess,
		Cwd:      cwd,
		AgentDir: dir,
	})
	wireQueues(e)
	e.AdoptSession(sess)

	out := serveRPC(t, e, `{"type":"get_tree"}
{"type":"get_entries"}
{"type":"cycle_thinking_level"}
{"type":"cycle_model"}
{"type":"get_fork_messages"}
{"type":"quit"}
`)
	if e.Opts.Config.Thinking != "minimal" {
		t.Fatalf("thinking = %s want minimal", e.Opts.Config.Thinking)
	}
	s := out
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
	e := newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}})
	wireQueues(e)

	out := serveRPC(t, e, `{"type":"prompt","message":"look","images":[{"type":"image","data":"AAA","mimeType":"image/png"}]}
{"type":"quit"}
`)
	rows := decodeRPCRows(t, out)
	var user map[string]any
	for _, s := range rpcRowsOfType(rows, "message_start") {
		msg, _ := s["message"].(map[string]any)
		if msg["role"] == "user" {
			user = msg
			break
		}
	}
	if user == nil {
		t.Fatalf("missing user message_start in %s", out)
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
	e := newPongEngine(Options{
		Config:   config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
		Session:  sess,
		Cwd:      cwd,
		AgentDir: dir,
	})
	wireQueues(e)
	e.AdoptSession(sess)

	serveRPC(t, e, `{"type":"prompt","message":"look","images":[{"type":"image","data":"AAA","mimeType":"image/png"}]}
{"type":"quit"}
`)
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

func widePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 20, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

type staticImageTool struct{ out string }

func (staticImageTool) Name() string        { return "shot" }
func (staticImageTool) Description() string { return "shot" }
func (staticImageTool) Schema() map[string]any {
	return map[string]any{"type": "object"}
}
func (s staticImageTool) Execute(context.Context, map[string]any) (string, bool) { return s.out, false }

func TestRunPromptResizesAttachmentToModelProfile(t *testing.T) {
	width := 20
	models.RegisterProvider(models.ProviderSpec{
		ID: "resize-rt", DefaultAPI: "openai-completions", DefaultID: "vision",
		Models: []models.Model{{
			Provider: "resize-rt", ID: "vision",
			InputLimits: &models.InputLimits{Images: &models.ImageInputLimits{Resize: &models.ImageResize{
				MaxWidth: &width,
			}}},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("resize-rt") })

	var content string
	e := &Engine{
		Provider: "resize-rt",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "resize-rt", Model: "vision"}},
		Stream: func(ctx context.Context, req ai.Context, opt ai.Options) (*ai.EventStream, error) {
			if n := len(req.Messages); n > 0 {
				content = req.Messages[n-1].Content
			}
			return textReply("ok")(ctx, req, opt)
		},
	}
	wireQueues(e)
	stream := e.RunPrompt(context.Background(), nil, "look", []ai.ImageContent{{
		Type: "image", Data: widePNG(t, 80, 8), MimeType: "image/png",
	}})
	for range stream.Events() {
	}
	if !strings.Contains(content, "resized from 80x8 to 20x2") {
		t.Fatalf("prompt content = %q", content)
	}
}

func TestExecutorAppliesResizeProfileToToolResult(t *testing.T) {
	width := 10
	models.RegisterProvider(models.ProviderSpec{
		ID: "resize-tool", DefaultAPI: "openai-completions", DefaultID: "vision",
		Models: []models.Model{{
			Provider: "resize-tool", ID: "vision",
			InputLimits: &models.InputLimits{Images: &models.ImageInputLimits{Resize: &models.ImageResize{
				MaxWidth: &width,
			}}},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("resize-tool") })

	raw, err := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "shot"},
			{"type": "image", "data": widePNG(t, 40, 4), "mimeType": "image/png"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		Provider: "resize-tool",
		Tools:    tools.NewRegistry(staticImageTool{out: string(raw)}),
		Opts:     Options{Config: config.Config{Provider: "resize-tool", Model: "vision"}},
	}
	out, isErr := e.Executor().Execute(context.Background(), agent.ToolCall{Name: "shot"})
	if isErr {
		t.Fatalf("shot: %s", out)
	}
	if !strings.Contains(out, "resized from 40x4 to 10x1") {
		t.Fatalf("tool result = %s", out)
	}
}

func TestRPCSteerQueuesImages(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{SteeringMode: "one-at-a-time"}}}
	serveRPC(t, e, `{"type":"steer","message":"nudge","images":[{"type":"image","data":"BBB","mimeType":"image/jpeg"}]}
{"type":"quit"}
`)
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

func TestNewResolvesScopedModels(t *testing.T) {
	cfg := config.Config{
		Provider: "anthropic", Model: "claude-sonnet-4",
		EnabledModels: []string{"anthropic/claude-sonnet-4", "anthropic/claude-haiku-4"},
	}
	dir := t.TempDir()
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{"settings when CLI empty", Options{Config: cfg}, []string{"claude-sonnet-4", "claude-haiku-4"}},
		{"CLI models replace settings", Options{Config: cfg, Models: []string{"openai/gpt-4o"}}, []string{"gpt-4o"}},
		{"user config wins over overlay", Options{Config: cfg, UserConfig: &config.Config{EnabledModels: []string{"openai/gpt-4o"}}}, []string{"gpt-4o"}},
		{"nil user enabledModels is implicit all", Options{Config: cfg, UserConfig: &config.Config{}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			opts.AgentDir = dir
			opts.Cwd = t.TempDir()
			opts.Offline = true
			opts.NoTools = true
			opts.NoSkills = true
			opts.NoExtensions = true
			e, err := New(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			var got []string
			for _, m := range e.Scoped {
				got = append(got, m.ID)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("scoped = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetScopedModelsAndCycleOrder(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	e.SetScopedModels([]models.Spec{
		{Model: models.Model{Provider: "openai", ID: "gpt-4o"}},
		{Model: models.Model{Provider: "anthropic", ID: "claude-sonnet-4"}},
		{Model: models.Model{Provider: "anthropic", ID: "claude-haiku-4"}},
	})
	next, ok := e.CycleModel(false)
	if !ok || next.ID != "claude-haiku-4" {
		t.Fatalf("cycle from sonnet in custom order = %+v ok=%v", next, ok)
	}
}

func TestPersistEnabledModels(t *testing.T) {
	dir := t.TempDir()
	e := &Engine{Opts: Options{AgentDir: dir, Config: config.Config{Theme: "default"}}}
	ids := []string{"openai/gpt-4o"}
	if err := e.PersistEnabledModels(&ids); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.EnabledModels) != 1 || loaded.EnabledModels[0] != "openai/gpt-4o" {
		t.Fatalf("%v", loaded.EnabledModels)
	}
	ids = []string{}
	if err := e.PersistEnabledModels(&ids); err != nil {
		t.Fatal(err)
	}
	loaded, err = config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EnabledModels == nil || len(loaded.EnabledModels) != 0 {
		t.Fatalf("empty selection: %#v", loaded.EnabledModels)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enabledModels"`) {
		t.Fatalf("empty selection key missing: %s", b)
	}
	if err := e.PersistEnabledModels(nil); err != nil {
		t.Fatal(err)
	}
	loaded, err = config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EnabledModels != nil {
		t.Fatalf("clear: %v", loaded.EnabledModels)
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
	e := newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}, Session: sess, Cwd: cwd, AgentDir: dir})
	wireQueues(e)
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

func printErrorReply(msg string) ai.StreamFn {
	return func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{
			Role:         ai.RoleAssistant,
			StopReason:   ai.StopError,
			ErrorMessage: msg,
		}), nil
	}
}

func TestPrintTextErrorExit(t *testing.T) {
	e := &Engine{
		Stream:   printErrorReply("boom"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Model: "x"}},
	}
	wireQueues(e)
	err := e.PrintText(context.Background(), io.Discard, nil, "hi")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrintJSONWritesSessionHeader(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	e := newPongEngine(Options{Config: config.Config{Model: "x"}, Session: sess})
	wireQueues(e)
	var out bytes.Buffer
	if err := e.WriteSessionHeader(&out); err != nil {
		t.Fatal(err)
	}
	if err := e.PrintJSON(context.Background(), &out, nil, "hi", nil); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	var header map[string]any
	if err := dec.Decode(&header); err != nil {
		t.Fatal(err)
	}
	if header["type"] != "session" || header["id"] != sess.ID() {
		t.Fatalf("header=%v", header)
	}
}

func TestRPCClearQueue(t *testing.T) {
	e := newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}})
	e.PushSteer("steer-me")
	e.PushFollow("follow-me")
	e.PushFollow("follow-2")
	out := serveRPC(t, e, `{"id":"c1","type":"clear_queue"}
{"type":"quit"}
`)
	rows := decodeRPCRows(t, out)
	var got map[string]any
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "clear_queue" {
			got = r
			break
		}
	}
	if got == nil {
		t.Fatalf("missing clear_queue response in %s", out)
	}
	data, _ := got["data"].(map[string]any)
	steer, _ := data["steering"].([]any)
	follow, _ := data["followUp"].([]any)
	if len(steer) != 1 || steer[0] != "steer-me" || len(follow) != 2 || follow[0] != "follow-me" || follow[1] != "follow-2" {
		t.Fatalf("data=%v", data)
	}
	if n := e.pendingCount(); n != 0 {
		t.Fatalf("pending=%d", n)
	}
}

func TestRPCRejectedDuringCompaction(t *testing.T) {
	tests := []struct {
		name string
		e    *Engine
		in   string
	}{
		{"prompt", newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}), `{"type":"prompt","message":"hi"}`},
		{"steer", &Engine{Opts: Options{Config: config.Config{SteeringMode: "one-at-a-time"}}}, `{"type":"steer","message":"nudge"}`},
		{"follow_up", &Engine{Opts: Options{Config: config.Config{FollowUpMode: "one-at-a-time"}}}, `{"type":"follow_up","message":"later"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.e.setCompacting(true)
			var out bytes.Buffer
			if err := tt.e.ServeRPC(context.Background(), strings.NewReader(tt.in+"\n{\"type\":\"quit\"}\n"), &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), `"success":false`) || !strings.Contains(out.String(), "compaction") {
				t.Fatalf("expected %s failure: %s", tt.name, out.String())
			}
			if n := tt.e.pendingCount(); n != 0 {
				t.Fatalf("queued during compact: %d", n)
			}
		})
	}
}

func TestSetThinkingLevelRecordsSession(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	e := &Engine{Opts: Options{Session: sess, Config: config.Config{Thinking: "off"}}}
	if got := e.SetThinkingLevel("high", false); got != "high" {
		t.Fatalf("got %s", got)
	}
	found := false
	for _, en := range sess.Entries() {
		if en.Type == "thinking_level_change" && en.ThinkingLevel == "high" {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries=%+v", sess.Entries())
	}
}

func TestRPCGetAvailableThinkingLevelsOmitsUnmappedXHigh(t *testing.T) {
	e := &Engine{
		Provider: "anthropic",
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	out := serveRPC(t, e, `{"type":"get_available_thinking_levels"}
{"type":"quit"}
`)
	s := out
	if strings.Contains(s, `"xhigh"`) || strings.Contains(s, `"max"`) {
		t.Fatalf("xhigh/max should be omitted: %s", s)
	}
	if !strings.Contains(s, `"high"`) {
		t.Fatalf("missing high: %s", s)
	}
}

func TestBoundStreamUsesModelsJSONCustomProvider(t *testing.T) {
	var gotAuth, gotPath, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		gotModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	t.Cleanup(func() {
		auth.UnregisterProvider("co-stream")
		models.ClearOverlays()
		models.UnregisterProvider("co-stream")
	})
	body := `{
  "providers": {
    "co-stream": {
      "baseUrl": "` + srv.URL + `/v1",
      "api": "openai-completions",
      "apiKey": "sk-test",
      "models": [{"id": "GLM-5.3"}]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := New(context.Background(), Options{
		Cwd:          t.TempDir(),
		AgentDir:     dir,
		Offline:      true,
		NoExtensions: true,
		NoTools:      true,
		CLIProvider:  "co-stream",
		CLIModel:     "GLM-5.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.Provider != "co-stream" {
		t.Fatalf("provider = %q", e.Provider)
	}
	stream, err := e.Stream(context.Background(), ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	}, ai.Options{Model: "GLM-5.3", Provider: "co-stream"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "ok" {
		t.Fatalf("final = %+v", final)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotModel != "GLM-5.3" {
		t.Fatalf("model = %q", gotModel)
	}
}
