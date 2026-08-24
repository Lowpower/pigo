package ai

// EventType enumerates the AssistantMessageEvent variants (pi types.ts ~L535).
// Names are snake_case to match pi exactly.
type EventType string

// AssistantMessageEvent variant tags (pi types.ts ~L535).
const (
	EventStart         EventType = "start"
	EventTextStart     EventType = "text_start"
	EventTextDelta     EventType = "text_delta"
	EventTextEnd       EventType = "text_end"
	EventThinkingStart EventType = "thinking_start"
	EventThinkingDelta EventType = "thinking_delta"
	EventThinkingEnd   EventType = "thinking_end"
	EventToolCallStart EventType = "toolcall_start"
	EventToolCallDelta EventType = "toolcall_delta"
	EventToolCallEnd   EventType = "toolcall_end"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

// Event is a single streamed AssistantMessageEvent. It is a flat tagged union:
// Type selects which fields are meaningful. Every partial-carrying event includes
// ContentIndex and Partial (a pointer to the evolving message, as in pi).
type Event struct {
	Type EventType

	// Set on *_start / *_delta / *_end events.
	ContentIndex int

	// Set on text_delta / thinking_delta / toolcall_delta.
	Delta string

	// Set on text_end / thinking_end (the finalized block content).
	Content string

	// Set on toolcall_end (the finalized tool call).
	ToolCall *Content

	// Set on every event except done/error: the in-progress message.
	Partial *AssistantMessage

	// Set on done/error: the terminal reason and final message.
	Reason  StopReason
	Message *AssistantMessage
}
