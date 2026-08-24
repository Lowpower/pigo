package ai

import (
	"context"
	"strings"
	"testing"
)

func TestScriptedStreamFn(t *testing.T) {
	sf := ScriptedStreamFn("hello there world", 0)
	stream, err := sf(context.Background(), Context{}, Options{Model: "mock"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	events, final := stream.Collect()

	if len(events) < 3 {
		t.Fatalf("too few events: %d", len(events))
	}
	if events[0].Type != EventStart || events[1].Type != EventTextStart {
		t.Errorf("first events = %v, %v", events[0].Type, events[1].Type)
	}
	if final == nil || final.Text() != "hello there world" {
		t.Fatalf("final text = %v, want %q", final, "hello there world")
	}
	if final.StopReason != StopStop {
		t.Errorf("stopReason = %q, want stop", final.StopReason)
	}
}

func TestEchoStreamFn(t *testing.T) {
	sf := EchoStreamFn()
	stream, err := sf(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "ping"}},
	}, Options{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	_, final := stream.Collect()
	if final == nil {
		t.Fatal("no final message")
	}
	if got := final.Text(); got == "" || !strings.Contains(got, "ping") {
		t.Errorf("echo text = %q, want it to contain the user message", got)
	}
}
