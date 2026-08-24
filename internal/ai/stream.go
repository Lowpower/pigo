package ai

import "context"

// Role values for messages sent to a provider.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is a single conversation message in a request Context.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Tool is a provider-neutral tool definition (JSON Schema parameters).
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Context is the request context passed to a StreamFn (pi's Context:
// systemPrompt + messages + tools).
type Context struct {
	System   string
	Messages []Message
	Tools    []Tool
}

// Options carries per-request knobs. Kept minimal for Phase 1.
type Options struct {
	Model     string
	MaxTokens int
}

// StreamFn is the spine abstraction: given a request context it returns a stream
// of AssistantMessageEvents. This is the Go form of pi's StreamFn
// (packages/agent/src/types.ts ~L28). Request/model failures are encoded in the
// stream (a terminal error event), not returned as err; err is reserved for
// setup failures before any event is produced.
type StreamFn func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error)

// EventStream is a channel-backed stream of events, closed when the producer is
// done. It mirrors pi's AssistantMessageEventStream (a push stream consumers
// range over).
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

// end closes the stream. It must be called exactly once by the producer.
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
