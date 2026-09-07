package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/tools"
)

type streamingExec struct{}

func (streamingExec) Execute(ctx context.Context, _ ToolCall) (string, bool) {
	if fn := tools.OutputUpdate(ctx); fn != nil {
		fn("partial-out")
	}
	return "done", false
}

func TestLoopEmitsToolExecutionUpdate(t *testing.T) {
	provider := scriptedProvider(
		toolCallMessage("tc1", "bash", map[string]any{"command": "echo hi"}),
		textMessage("ok"),
	)
	events := Run(context.Background(), provider, ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "run"}},
	}, streamingExec{}, Config{Model: "test"}).Collect()

	var update Event
	for _, e := range events {
		if e.Type == EventToolUpdate {
			update = e
			break
		}
	}
	if update.Type != EventToolUpdate {
		t.Fatalf("missing tool_execution_update in %#v", eventTypes(events))
	}
	if update.ToolCallID != "tc1" || update.ToolName != "bash" || update.Result != "partial-out" {
		t.Fatalf("update = %+v", update)
	}
	if update.Args["command"] != "echo hi" {
		t.Fatalf("args = %#v", update.Args)
	}
}

func TestOnMessageEndReplacesAssistant(t *testing.T) {
	var life []EventType
	events := Run(context.Background(), scriptedProvider(textMessage("hello")), ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	}, nil, Config{
		Model: "test",
		OnLifecycle: func(ev Event) {
			life = append(life, ev.Type)
		},
		OnMessageEnd: func(m *ai.AssistantMessage) *ai.AssistantMessage {
			cp := *m
			cp.ErrorMessage = "patched"
			return &cp
		},
	}).Collect()

	var saw bool
	for _, ev := range events {
		if ev.Type == EventMessageEnd && ev.Assistant != nil && ev.Assistant.ErrorMessage == "patched" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("message_end not patched in %#v", eventTypes(events))
	}
	want := map[EventType]bool{
		EventAgentStart: true, EventTurnStart: true, EventMessageStart: true,
		EventMessageUpdate: true, EventMessageEnd: true, EventTurnEnd: true, EventAgentEnd: true,
	}
	for _, tpe := range life {
		delete(want, tpe)
	}
	if len(want) > 0 {
		t.Fatalf("OnLifecycle missing %v; got %v", want, life)
	}
}

func TestToJSONToolExecutionUpdate(t *testing.T) {
	got, err := ToJSON(Event{
		Type:       EventToolUpdate,
		ToolCallID: "call_1",
		ToolName:   "bash",
		Args:       map[string]any{"command": "ls"},
		Result:     "partial output so far...",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "tool_execution_update" {
		t.Fatalf("type = %v", payload["type"])
	}
	if payload["toolCallId"] != "call_1" || payload["toolName"] != "bash" {
		t.Fatalf("ids = %s", raw)
	}
	partial, _ := payload["partialResult"].(map[string]any)
	content, _ := partial["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("partialResult = %s", raw)
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "partial output so far..." {
		t.Fatalf("content = %s", raw)
	}
}

func eventTypes(events []Event) []EventType {
	out := make([]EventType, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}
