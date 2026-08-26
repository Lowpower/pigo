package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
)

func TestRPCGetStatePendingMessageCount(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{SteeringMode: "all"}}}
	e.PushSteer("one")
	e.PushFollow("two")
	in := strings.NewReader(`{"type":"get_state"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	var data map[string]any
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "get_state" {
			data, _ = r["data"].(map[string]any)
		}
	}
	if data["pendingMessageCount"] != float64(2) {
		t.Fatalf("pendingMessageCount = %#v in %s", data["pendingMessageCount"], out.String())
	}
	if data["isCompacting"] != false {
		t.Fatalf("isCompacting = %#v", data["isCompacting"])
	}
}

func TestRPCNewSessionParentSession(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	e := &Engine{Opts: Options{Cwd: cwd, AgentDir: dir, Session: sess}}
	parent := "/tmp/parent.jsonl"
	in := strings.NewReader(`{"type":"new_session","parentSession":"/tmp/parent.jsonl"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if e.Opts.Session == nil || e.Opts.Session.ParentSession() != parent {
		t.Fatalf("parentSession = %q", e.Opts.Session.ParentSession())
	}
	if e.Opts.Session.ID() == sess.ID() {
		t.Fatal("new_session did not replace the session")
	}
}

func TestRPCGetSessionStatsShape(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	asst := &ai.AssistantMessage{
		Role: ai.RoleAssistant,
		Content: []*ai.Content{
			{Type: ai.KindText, Text: "yo"},
			{Type: ai.KindToolCall, ToolID: "c1", ToolName: "read"},
		},
		Usage: ai.Usage{
			Input: 10, Output: 5, CacheRead: 2, CacheWrite: 1, TotalTokens: 18,
			Cost: ai.UsageCost{Input: 0.1, Output: 0.4, CacheRead: 0.02, CacheWrite: 0.08, Total: 0.6},
		},
	}
	if _, err := sess.AppendMessage("assistant", asst); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("toolResult", map[string]any{"role": "toolResult", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Opts: Options{Cwd: cwd, AgentDir: dir, Session: sess, Config: config.Config{ContextWindow: 1000}}}
	in := strings.NewReader(`{"type":"get_session_stats"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	var data map[string]any
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "get_session_stats" {
			data, _ = r["data"].(map[string]any)
		}
	}
	if data == nil {
		t.Fatalf("missing stats in %s", out.String())
	}
	if data["userMessages"] != float64(1) || data["assistantMessages"] != float64(1) || data["toolResults"] != float64(1) || data["toolCalls"] != float64(1) {
		t.Fatalf("counts = %#v", data)
	}
	tokens, _ := data["tokens"].(map[string]any)
	if tokens["input"] != float64(10) || tokens["output"] != float64(5) || tokens["total"] != float64(18) {
		t.Fatalf("tokens = %#v", tokens)
	}
	if data["cost"] != 0.6 {
		t.Fatalf("cost = %#v want 0.6", data["cost"])
	}
	usage, _ := data["contextUsage"].(map[string]any)
	if usage["contextWindow"] != float64(1000) {
		t.Fatalf("contextUsage = %#v", usage)
	}
}

func TestRPCCompactCustomInstructions(t *testing.T) {
	var prompt string
	e := &Engine{
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			if len(req.Messages) > 0 {
				prompt = req.Messages[len(req.Messages)-1].Content
			}
			return ai.ScriptedStreamFn("## Goal\nFocus.", 0)(ctx, req, opts)
		},
		Opts: Options{Config: config.Config{KeepRecentTokens: 1}},
	}
	body := `{"type":"compact","customInstructions":"Focus on code changes"}
{"type":"quit"}
`
	// Seed history via a prompt-less path: compact uses ServeRPC's empty history.
	// Put messages into a session so History() is non-empty.
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	for i := 0; i < 8; i++ {
		if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": strings.Repeat("x", 80)}); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": strings.Repeat("y", 80)}); err != nil {
			t.Fatal(err)
		}
	}
	e.Opts.Session = sess
	e.Opts.Cwd = cwd
	e.Opts.AgentDir = dir
	in := strings.NewReader(body)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Additional focus: Focus on code changes") {
		raw, _ := json.Marshal(decodeRPCRows(t, out.String()))
		t.Fatalf("custom instructions not in prompt %q\nout=%s", prompt, raw)
	}
}
