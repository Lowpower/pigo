// Package ai is the streaming spine of pigo: the provider-neutral event model
// (AssistantMessageEvent), the StreamFn abstraction, incremental JSON parsing for
// streamed tool-call arguments, and the per-provider adapters.
//
// Ported from pi's packages/ai/src (types.ts, utils/json-parse.ts, api/*). Event
// and field names follow the pi source (snake_case event types; usage carried on
// the final message, not as a separate chunk).
package ai

// StopReason mirrors pi's StopReason (packages/ai/src/types.ts).
type StopReason string

// StopReason values (pi types.ts ~L405).
const (
	StopPending  StopReason = "pending"
	StopStop     StopReason = "stop"
	StopLength   StopReason = "length"
	StopToolUse  StopReason = "toolUse"
	StopError    StopReason = "error"
	StopAborted  StopReason = "aborted"
	StopDeferred StopReason = "deferred"
)

// ContentKind discriminates the content blocks of an AssistantMessage.
type ContentKind string

// Content block kinds (pi types.ts: TextContent / ThinkingContent / ToolCall).
const (
	KindText     ContentKind = "text"
	KindThinking ContentKind = "thinking"
	KindToolCall ContentKind = "toolCall"
)

// Content is one block of an assistant message. It is a flat struct (rather than
// an interface) so streaming code can address blocks by index and mutate them in
// place, matching pi's anthropic-messages.ts block handling.
type Content struct {
	Type ContentKind `json:"type"`

	// KindText
	Text string `json:"text,omitempty"`

	// KindThinking
	Thinking          string `json:"thinking,omitempty"`
	ThinkingSignature string `json:"thinkingSignature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`

	// KindToolCall
	ToolID    string         `json:"id,omitempty"`
	ToolName  string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`

	// partialJSON is a streaming scratch buffer for tool-call argument deltas.
	// It is never serialized (matching pi, which strips it before persisting).
	partialJSON string
}

// Usage mirrors pi's Usage (token counts).
type Usage struct {
	Input        int `json:"input"`
	Output       int `json:"output"`
	CacheRead    int `json:"cacheRead"`
	CacheWrite   int `json:"cacheWrite"`
	CacheWrite1h int `json:"cacheWrite1h,omitempty"`
	Reasoning    int `json:"reasoning,omitempty"`
	TotalTokens  int `json:"totalTokens"`
}

// AssistantMessage mirrors pi's AssistantMessage.
type AssistantMessage struct {
	Role          string     `json:"role"`
	Content       []*Content `json:"content"`
	API           string     `json:"api,omitempty"`
	Provider      string     `json:"provider,omitempty"`
	Model         string     `json:"model,omitempty"`
	ResponseID    string     `json:"responseId,omitempty"`
	Usage         Usage      `json:"usage"`
	StopReason    StopReason `json:"stopReason"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
	RawStopReason string     `json:"rawStopReason,omitempty"`
}

// Text returns the concatenated text of all text blocks.
func (m *AssistantMessage) Text() string {
	var s string
	for _, c := range m.Content {
		if c.Type == KindText {
			s += c.Text
		}
	}
	return s
}

// ToolCalls returns the tool-call blocks of the message.
func (m *AssistantMessage) ToolCalls() []*Content {
	var out []*Content
	for _, c := range m.Content {
		if c.Type == KindToolCall {
			out = append(out, c)
		}
	}
	return out
}
