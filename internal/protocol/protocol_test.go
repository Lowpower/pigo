package protocol

import (
	"bytes"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	msgs := []Message{
		{Type: TypeHello, ExtName: "demo", APIVersion: 1},
		{Type: TypeRegisterTool, Name: "reverse", Description: "reverse a string", Schema: map[string]any{"type": "object"}},
		{Type: TypeToolCall, ID: "1", Name: "reverse", Args: map[string]any{"text": "abc\nwith newline"}},
		{Type: TypeToolResult, ID: "1", Result: "cba", IsError: false},
		{Type: TypeEvent, Event: "before_tool_call", Payload: map[string]any{"tool": "read"}},
		{Type: TypeEventResult, ID: "e1", Payload: map[string]any{"block": true}},
		{Type: TypeRegisterCommand, Name: "stats", Description: "show stats"},
		{Type: TypeRegisterShortcut, Name: "ctrl+shift+p", Description: "toggle"},
		{Type: TypeRegisterFlag, Name: "plan", Description: "plan mode", Args: map[string]any{"type": "boolean"}},
		{Type: TypeGetFlag, ID: "f1", Name: "plan"},
		{Type: TypeFlagValue, ID: "f1", Payload: map[string]any{"value": true}},
		{Type: TypeShortcut, Name: "ctrl+shift+p"},
		{Type: TypeCommand, Name: "stats", Text: "today"},
		{Type: TypeRegisterProvider, Name: "local", Args: map[string]any{"baseUrl": "http://127.0.0.1"}},
		{Type: TypeUnregisterProvider, Name: "local"},
		{Type: TypeOAuthLogin, ID: "o1"},
		{Type: TypeOAuthResult, ID: "o1", Payload: map[string]any{"access": "tok"}},
		{Type: TypeRefreshModels, ID: "r1"},
		{Type: TypeRefreshModelsResult, ID: "r1", Payload: map[string]any{"models": []any{}}},
		{Type: TypeStreamStart, ID: "s1", Payload: map[string]any{"model": "x"}},
		{Type: TypeStreamEvent, ID: "s1", Event: "text_delta", Payload: map[string]any{"delta": "hi"}},
		{Type: TypeStreamAbort, ID: "s1"},
		{Type: TypeSetActiveTools, Payload: map[string]any{"names": []any{"tool_search", "lookup"}}},
		{Type: TypeHostRequest, ID: "h1", Name: "isIdle", Args: map[string]any{}},
		{Type: TypeHostResult, ID: "h1", Args: map[string]any{"idle": true}},
		{Type: TypeHostEvent, ID: "e2", Name: "custom.submit", Args: map[string]any{"id": "w1"}},
		{Type: TypeHostEventResult, ID: "e2", Args: map[string]any{}},
		{Type: TypeShutdownRequest},
	}

	var buf bytes.Buffer
	for _, m := range msgs {
		if err := WriteMessage(&buf, m); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	for i, want := range msgs {
		got, err := ReadMessage(&buf)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if got.Type != want.Type || got.Name != want.Name || got.ID != want.ID || got.Result != want.Result {
			t.Errorf("msg %d = %+v, want %+v", i, got, want)
		}
	}
	if _, err := ReadMessage(&buf); err != io.EOF {
		t.Errorf("expected EOF after draining, got %v", err)
	}
}

func TestEventResultKeepsEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, Message{Type: TypeEventResult, ID: "e1", Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Payload == nil {
		t.Fatal("empty event_result payload was dropped")
	}
	if len(got.Payload) != 0 {
		t.Fatalf("payload = %#v", got.Payload)
	}
}

func TestReadMessageRejectsHugeFrame(t *testing.T) {
	// length prefix claims > MaxFrameLen
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 0xff, 0xff, 0xff})
	if _, err := ReadMessage(&buf); err == nil {
		t.Error("expected error for oversized frame")
	}
}
