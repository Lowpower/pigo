package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
)

func toolCallMessage(id, name string, args map[string]any) *ai.AssistantMessage {
	return &ai.AssistantMessage{
		Role:       ai.RoleAssistant,
		StopReason: ai.StopToolUse,
		Content:    []*ai.Content{{Type: ai.KindToolCall, ToolID: id, ToolName: name, Arguments: args}},
	}
}

func textMessage(text string) *ai.AssistantMessage {
	return &ai.AssistantMessage{
		Role:       ai.RoleAssistant,
		StopReason: ai.StopStop,
		Content:    []*ai.Content{{Type: ai.KindText, Text: text}},
	}
}

// scriptedProvider returns each message in turn (repeating the last).
func scriptedProvider(msgs ...*ai.AssistantMessage) ai.StreamFn {
	var i int32
	return func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		n := int(atomic.AddInt32(&i, 1)) - 1
		m := msgs[len(msgs)-1]
		if n < len(msgs) {
			m = msgs[n]
		}
		return ai.EmitMessage(ctx, m), nil
	}
}

func countType(events []Event, t EventType) int {
	n := 0
	for _, e := range events {
		if e.Type == t {
			n++
		}
	}
	return n
}

func TestLoopMultiTurnToolCycle(t *testing.T) {
	provider := scriptedProvider(
		toolCallMessage("tc1", "read", map[string]any{"path": "README.md"}),
		textMessage("Done."),
	)

	var mu sync.Mutex
	var calls []ToolCall
	exec := ToolFunc(func(_ context.Context, c ToolCall) (string, bool) {
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		return "file body", false
	})

	reqCtx := ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "read the readme"}}}
	events := Run(context.Background(), provider, reqCtx, exec, Config{Model: "test"}).Collect()

	if events[0].Type != EventAgentStart {
		t.Fatalf("first event = %q, want agent_start", events[0].Type)
	}
	last := events[len(events)-1]
	if last.Type != EventAgentEnd {
		t.Fatalf("last event = %q, want agent_end", last.Type)
	}

	if got := countType(events, EventTurnStart); got != 2 {
		t.Errorf("turn_start count = %d, want 2", got)
	}
	if got := countType(events, EventTurnEnd); got != 2 {
		t.Errorf("turn_end count = %d, want 2", got)
	}
	if got := countType(events, EventToolStart); got != 1 {
		t.Errorf("tool_execution_start count = %d, want 1", got)
	}

	if len(calls) != 1 || calls[0].Name != "read" || calls[0].Args["path"] != "README.md" {
		t.Fatalf("executor calls = %+v, want one read of README.md", calls)
	}

	// Transcript: user, assistant(tool), toolResult, assistant(text).
	roles := make([]string, 0, len(last.Messages))
	for _, m := range last.Messages {
		roles = append(roles, m.Role)
	}
	want := []string{RoleUser, RoleAssistant, RoleToolResult, RoleAssistant}
	if len(roles) != len(want) {
		t.Fatalf("transcript roles = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("transcript roles = %v, want %v", roles, want)
		}
	}
}

func TestLoopEventTrace(t *testing.T) {
	provider := scriptedProvider(
		toolCallMessage("tc1", "read", map[string]any{"path": "README.md"}),
		textMessage("Done."),
	)
	exec := ToolFunc(func(_ context.Context, _ ToolCall) (string, bool) { return "file body", false })

	reqCtx := ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "read the readme"}}}
	events := Run(context.Background(), provider, reqCtx, exec, Config{Model: "test", ToolExecution: Sequential}).Collect()

	for i, e := range events {
		detail := ""
		switch e.Type {
		case EventMessageUpdate:
			if e.AIEvent != nil {
				detail = "ai:" + string(e.AIEvent.Type)
			}
		case EventToolStart, EventToolEnd:
			detail = e.ToolName
		case EventAgentEnd:
			detail = fmt.Sprintf("%d messages", len(e.Messages))
		}
		t.Logf("%2d  %-22s %s", i, e.Type, detail)
	}

	got := make([]EventType, len(events))
	for i, e := range events {
		got[i] = e.Type
	}
	mu := EventMessageUpdate
	want := []EventType{
		EventAgentStart,
		EventTurnStart, EventMessageStart, mu, mu, mu, mu, mu, EventMessageEnd,
		EventToolStart, EventToolEnd, EventTurnEnd,
		EventTurnStart, EventMessageStart, mu, mu, mu, mu, mu, EventMessageEnd, EventTurnEnd,
		EventAgentEnd,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("event sequence =\n  %v\nwant\n  %v", got, want)
	}
}

func TestLoopParallelToolExecutionSourceOrder(t *testing.T) {
	twoCalls := &ai.AssistantMessage{
		Role:       ai.RoleAssistant,
		StopReason: ai.StopToolUse,
		Content: []*ai.Content{
			{Type: ai.KindToolCall, ToolID: "a", ToolName: "slow", Arguments: map[string]any{"n": float64(1)}},
			{Type: ai.KindToolCall, ToolID: "b", ToolName: "fast", Arguments: map[string]any{"n": float64(2)}},
		},
	}
	provider := scriptedProvider(twoCalls, textMessage("done"))

	// "slow" sleeps so it finishes after "fast": completion order != source order.
	exec := ToolFunc(func(_ context.Context, c ToolCall) (string, bool) {
		if c.Name == "slow" {
			time.Sleep(30 * time.Millisecond)
		}
		return "result-" + c.Name, false
	})

	reqCtx := ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "go"}}}
	events := Run(context.Background(), provider, reqCtx, exec, Config{Model: "test", ToolExecution: Parallel}).Collect()

	if got := countType(events, EventToolStart); got != 2 {
		t.Errorf("tool_execution_start count = %d, want 2", got)
	}

	// turn_end tool results must be in source order (a then b) despite completion order.
	var results []Msg
	for _, e := range events {
		if e.Type == EventTurnEnd && len(e.ToolResults) > 0 {
			results = e.ToolResults
			break
		}
	}
	if len(results) != 2 || results[0].ToolName != "slow" || results[1].ToolName != "fast" {
		t.Fatalf("tool results = %+v, want [slow, fast] in source order", results)
	}
	if results[0].Text != "result-slow" || results[1].Text != "result-fast" {
		t.Errorf("tool result texts = %q, %q", results[0].Text, results[1].Text)
	}
}

func TestLoopSequentialExecutionOrder(t *testing.T) {
	twoCalls := &ai.AssistantMessage{
		Role:       ai.RoleAssistant,
		StopReason: ai.StopToolUse,
		Content: []*ai.Content{
			{Type: ai.KindToolCall, ToolID: "a", ToolName: "first"},
			{Type: ai.KindToolCall, ToolID: "b", ToolName: "second"},
		},
	}
	provider := scriptedProvider(twoCalls, textMessage("done"))

	var order []string
	exec := ToolFunc(func(_ context.Context, c ToolCall) (string, bool) {
		order = append(order, c.Name) // safe: sequential mode runs one at a time
		return "ok", false
	})

	reqCtx := ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "go"}}}
	Run(context.Background(), provider, reqCtx, exec, Config{Model: "test", ToolExecution: Sequential}).Collect()

	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("sequential order = %v, want [first second]", order)
	}
}

func TestLoopContextCancellation(t *testing.T) {
	// A provider that would loop forever on tools; cancellation must stop it.
	provider := scriptedProvider(toolCallMessage("tc", "loop", nil))
	exec := ToolFunc(func(_ context.Context, _ ToolCall) (string, bool) { return "again", false })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := Run(ctx, provider, ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "go"}}}, exec, Config{Model: "test"})

	// Consume a few events, then cancel and ensure the stream closes (no hang).
	got := 0
	for range stream.Events() {
		got++
		if got == 3 {
			cancel()
		}
	}
	if got < 1 {
		t.Fatal("expected at least one event before cancellation")
	}
}

func TestLoopReplaysToolResultsWithIDs(t *testing.T) {
	var second []ai.Message
	var i int32
	provider := func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
		n := int(atomic.AddInt32(&i, 1))
		if n == 2 {
			second = append([]ai.Message(nil), req.Messages...)
		}
		if n == 1 {
			return ai.EmitMessage(ctx, toolCallMessage("tu_1", "read", map[string]any{"path": "README.md"})), nil
		}
		return ai.EmitMessage(ctx, textMessage("done")), nil
	}
	exec := ToolFunc(func(_ context.Context, _ ToolCall) (string, bool) { return "file body", false })
	Run(context.Background(), provider, ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "read it"}}}, exec, Config{Model: "test"}).Collect()

	if len(second) < 3 {
		t.Fatalf("second-turn messages = %d, want >= 3 (user, assistant, toolResult)", len(second))
	}
	var asst, tool ai.Message
	for _, m := range second {
		if m.Assistant != nil {
			asst = m
		}
		if m.Role == ai.RoleToolResult {
			tool = m
		}
	}
	if asst.Assistant == nil || len(asst.Assistant.ToolCalls()) != 1 || asst.Assistant.ToolCalls()[0].ToolID != "tu_1" {
		t.Fatalf("assistant replay missing tool call id: %+v", asst.Assistant)
	}
	if tool.ToolCallID != "tu_1" || tool.Content != "file body" {
		t.Fatalf("toolResult = role=%s id=%s content=%q", tool.Role, tool.ToolCallID, tool.Content)
	}
	wire := ai.AnthropicWireMessages(second)
	found := false
	for _, w := range wire {
		blocks, _ := w["content"].([]map[string]any)
		for _, b := range blocks {
			if b["type"] == "tool_result" && b["tool_use_id"] == "tu_1" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("Anthropic wire form lost tool_use_id pairing: %#v", wire)
	}
}

func TestLoopSeedsAssistantToolBlocksOnNewRun(t *testing.T) {
	exec := ToolFunc(func(_ context.Context, _ ToolCall) (string, bool) { return "file body", false })
	first := Run(context.Background(), scriptedProvider(
		toolCallMessage("tu_seed", "read", map[string]any{"path": "README.md"}),
		textMessage("done"),
	), ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "read it"}}}, exec, Config{Model: "test"}).Collect()
	var transcript []Msg
	for _, ev := range first {
		if ev.Type == EventAgentEnd {
			transcript = ev.Messages
		}
	}
	seed := MessagesFromTranscript(transcript)

	var seen []ai.Message
	var n int32
	provider := func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
		if atomic.AddInt32(&n, 1) == 1 {
			seen = append([]ai.Message(nil), req.Messages...)
		}
		return ai.EmitMessage(ctx, textMessage("ok")), nil
	}
	next := append(seed, ai.Message{Role: ai.RoleUser, Content: "continue"})
	Run(context.Background(), provider, ai.Context{Messages: next}, exec, Config{Model: "test"}).Collect()

	var asst, tool ai.Message
	for _, m := range seen {
		if m.Assistant != nil && len(m.Assistant.ToolCalls()) > 0 {
			asst = m
		}
		if m.Role == ai.RoleToolResult {
			tool = m
		}
	}
	if asst.Assistant == nil || len(asst.Assistant.ToolCalls()) != 1 || asst.Assistant.ToolCalls()[0].ToolID != "tu_seed" {
		t.Fatalf("seeded assistant lost tool call: %+v", asst.Assistant)
	}
	if tool.ToolCallID != "tu_seed" {
		t.Fatalf("seeded toolResult id = %q", tool.ToolCallID)
	}
}

func TestLoopLengthStopDoesNotExecuteTools(t *testing.T) {
	called := int32(0)
	exec := ToolFunc(func(_ context.Context, _ ToolCall) (string, bool) {
		atomic.AddInt32(&called, 1)
		return "should not run", false
	})
	truncated := &ai.AssistantMessage{
		Role:       ai.RoleAssistant,
		StopReason: ai.StopLength,
		Content:    []*ai.Content{{Type: ai.KindToolCall, ToolID: "tu_x", ToolName: "bash", Arguments: map[string]any{"command": "rm -rf /"}}},
	}
	events := Run(context.Background(), scriptedProvider(truncated, textMessage("recovered")), ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "go"}}}, exec, Config{Model: "test"}).Collect()
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("truncated tool call was executed")
	}
	var sawErr bool
	for _, ev := range events {
		if ev.Type == EventToolEnd && ev.IsError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("expected error tool_end for truncated call")
	}
}

func TestLoopInjectsSteeringBeforeNextLLMCall(t *testing.T) {
	var mu sync.Mutex
	var second []ai.Message
	var n int32
	provider := func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
		i := atomic.AddInt32(&n, 1)
		if i == 2 {
			mu.Lock()
			second = append([]ai.Message(nil), req.Messages...)
			mu.Unlock()
			return ai.EmitMessage(ctx, textMessage("after steer")), nil
		}
		return ai.EmitMessage(ctx, toolCallMessage("t1", "read", map[string]any{"path": "x"})), nil
	}
	exec := ToolFunc(func(_ context.Context, _ ToolCall) (string, bool) { return "ok", false })
	steered := int32(0)
	cfg := Config{Model: "test", Steering: func() []ai.Message {
		if atomic.AddInt32(&steered, 1) == 1 {
			return nil // after first turn setup
		}
		return []ai.Message{{Role: ai.RoleUser, Content: "steer now"}}
	}}
	Run(context.Background(), provider, ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "go"}}}, exec, cfg).Collect()
	found := false
	for _, m := range second {
		if m.Role == ai.RoleUser && m.Content == "steer now" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steering message missing from second LLM call: %+v", second)
	}
}
