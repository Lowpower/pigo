package agent

import (
	"context"

	"github.com/Lowpower/pigo/internal/ai"
)

// Message roles in the agent transcript.
const (
	RoleUser       = "user"
	RoleAssistant  = "assistant"
	RoleToolResult = "toolResult"
)

// Msg is one entry in the agent transcript (pi's AgentMessage: user / assistant /
// toolResult).
type Msg struct {
	Role string `json:"role"`

	// RoleUser: the prompt text. RoleToolResult: the tool output text.
	Text string `json:"text,omitempty"`

	// RoleAssistant: the streamed assistant message.
	Assistant *ai.AssistantMessage `json:"assistant,omitempty"`

	// RoleToolResult metadata.
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	IsError    bool   `json:"isError,omitempty"`
}

// EventType enumerates AgentEvent variants (pi packages/agent/src/types.ts ~L428).
type EventType string

// AgentEvent variant tags.
const (
	EventAgentStart    EventType = "agent_start"
	EventAgentEnd      EventType = "agent_end"
	EventTurnStart     EventType = "turn_start"
	EventTurnEnd       EventType = "turn_end"
	EventMessageStart  EventType = "message_start"
	EventMessageUpdate EventType = "message_update"
	EventMessageEnd    EventType = "message_end"
	EventToolStart     EventType = "tool_execution_start"
	EventToolEnd       EventType = "tool_execution_end"
)

// Event is a single AgentEvent. Type selects which fields are meaningful.
type Event struct {
	Type EventType

	// message_start / message_update / message_end / turn_end: the assistant message.
	Assistant *ai.AssistantMessage
	// message_update: the underlying provider event.
	AIEvent *ai.Event

	// tool_execution_start / tool_execution_end.
	ToolCallID string
	ToolName   string
	Args       map[string]any
	Result     string
	IsError    bool

	// turn_end: the tool results produced this turn.
	ToolResults []Msg
	// agent_end: the full transcript.
	Messages []Msg
}

// Stream is a channel-backed stream of AgentEvents (pi's EventStream<AgentEvent>).
type Stream struct {
	ch chan Event
}

func newStream(buffer int) *Stream { return &Stream{ch: make(chan Event, buffer)} }

func (s *Stream) push(ctx context.Context, ev Event) bool {
	select {
	case s.ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *Stream) end() { close(s.ch) }

// Events returns the receive channel to range over.
func (s *Stream) Events() <-chan Event { return s.ch }

// Collect drains the stream and returns all events.
func (s *Stream) Collect() []Event {
	var out []Event
	for ev := range s.ch {
		out = append(out, ev)
	}
	return out
}
