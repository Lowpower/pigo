package ai

import "context"

// Role values for messages sent to a provider. RoleToolResult is "toolResult";
// providers map it onto their wire format.
const (
	RoleUser       = "user"
	RoleAssistant  = "assistant"
	RoleToolResult = "toolResult"
	RoleTool       = "tool"
)

// Message is a single conversation message in a request Context.
// Content is the plain-text body for user (and a fallback for others).
// Assistant, when set, is the full assistant message including tool-call blocks
// and must be replayed to the provider on subsequent turns.
// ToolCallID / ToolName / IsError describe a toolResult turn.
type Message struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	Assistant  *AssistantMessage `json:"assistant,omitempty"`
	ToolCallID string            `json:"toolCallId,omitempty"`
	ToolName   string            `json:"toolName,omitempty"`
	IsError    bool              `json:"isError,omitempty"`
	// Images are extra user-message blocks. Empty for text-only.
	Images []ImageContent `json:"images,omitempty"`
}

// ImageContent is a base64 image attached to a user (or tool-result) message.
// packages/ai/src/types.ts ImageContent
type ImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// Text returns the display/token-estimate text of the message.
func (m Message) Text() string {
	if m.Assistant != nil {
		if t := m.Assistant.Text(); t != "" {
			return t
		}
	}
	return m.Content
}

// Tool is a provider-neutral tool definition (JSON Schema parameters).
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Context is the request context passed to a StreamFn (system prompt, messages,
// tools).
type Context struct {
	System   string
	Messages []Message
	Tools    []Tool
}

// Options carries per-request knobs.
type Options struct {
	Model          string
	MaxTokens      int
	Thinking       string // off|minimal|low|medium|high|xhigh|max
	ThinkingBudget int    // token budget resolved from thinkingBudgets
	SessionID      string // coding session id; Codex reuses a WebSocket per session
	CacheRetention string // none|short|long; forwarded on pi-messages
	ToolChoice     string // auto|none|required; forwarded on pi-messages
}

// StreamFn is the spine abstraction: given a request context it returns a stream
// of AssistantMessageEvents. Request/model failures are encoded in the stream (a
// terminal error event), not returned as err; err is reserved for setup failures
// before any event is produced.
type StreamFn func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error)

// EventStream is a channel-backed stream of events, closed when the producer is
// done. Consumers range over it.
type EventStream struct {
	ch chan Event
}

// NewEventStream creates an EventStream with the given channel buffer.
func NewEventStream(buffer int) *EventStream {
	return &EventStream{ch: make(chan Event, buffer)}
}

// push sends an event to consumers. It respects ctx cancellation so a producer
// never blocks forever when the consumer has gone away.
func (s *EventStream) push(ctx context.Context, ev Event) bool {
	select {
	case s.ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// Push sends an event to consumers. False means ctx was cancelled.
func (s *EventStream) Push(ctx context.Context, ev Event) bool {
	return s.push(ctx, ev)
}

// Close ends the stream. It must be called exactly once by the producer.
func (s *EventStream) Close() { s.end() }

func (s *EventStream) end() { close(s.ch) }

// Events returns the receive channel to range over.
func (s *EventStream) Events() <-chan Event { return s.ch }

// Collect drains the stream and returns all events plus the terminal message
// (from the done or error event), if any. It is a convenience for tests and
// simple callers.
func (s *EventStream) Collect() ([]Event, *AssistantMessage) {
	var events []Event
	var final *AssistantMessage
	for ev := range s.ch {
		events = append(events, ev)
		if ev.Type == EventDone || ev.Type == EventError {
			final = ev.Message
			if final == nil {
				final = ev.Partial
			}
		}
	}
	return events, final
}
