// Package ai is the streaming spine of pigo: the provider-neutral event model
// (AssistantMessageEvent), the StreamFn abstraction, incremental JSON parsing for
// streamed tool-call arguments, and the per-provider adapters.
//
// Event types use snake_case; usage is carried on the final message, not as a
// separate chunk.
package ai

import "fmt"

// StopReason is why a stream ended.
type StopReason string

// StopReason values.
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

// Content block kinds.
const (
	KindText     ContentKind = "text"
	KindThinking ContentKind = "thinking"
	KindToolCall ContentKind = "toolCall"
)

// Content is one block of an assistant message. It is a flat struct (rather than
// an interface) so streaming code can address blocks by index and mutate them in
// place.
type Content struct {
	Type ContentKind `json:"type"`

	// KindText
	Text          string `json:"text,omitempty"`
	TextSignature string `json:"textSignature,omitempty"`

	// KindThinking
	Thinking          string `json:"thinking,omitempty"`
	ThinkingSignature string `json:"thinkingSignature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`

	// KindToolCall
	ToolID    string         `json:"id,omitempty"`
	ToolName  string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`

	// partialJSON is a streaming scratch buffer for tool-call argument deltas.
	// It is never serialized.
	partialJSON string
}

// UsageCost is the dollar breakdown on Usage.
type UsageCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// Usage holds token counts.
type Usage struct {
	Input        int       `json:"input"`
	Output       int       `json:"output"`
	CacheRead    int       `json:"cacheRead"`
	CacheWrite   int       `json:"cacheWrite"`
	CacheWrite1h int       `json:"cacheWrite1h,omitempty"`
	Reasoning    int       `json:"reasoning,omitempty"`
	TotalTokens  int       `json:"totalTokens"`
	Cost         UsageCost `json:"cost"`
}

// AssistantMessage is a completed assistant turn.
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

	// ProviderThinkingLevel is the effort actually sent on a
	// supportsMidConvoEffort turn. Empty on older sessions and other transports.
	ProviderThinkingLevel string `json:"providerThinkingLevel,omitempty"`

	// Diagnostics holds provider recovery details, including dropped thinking blocks.
	Diagnostics []AssistantDiagnostic `json:"diagnostics,omitempty"`
}

// InputTransformation is one Anthropic input_transformations entry.
type InputTransformation struct {
	Type   string `json:"type,omitempty"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// DiagnosticDetails is the payload of an assistant diagnostic.
type DiagnosticDetails struct {
	Transformations []InputTransformation `json:"transformations,omitempty"`
}

// AssistantDiagnostic is one persisted provider diagnostic on an assistant message.
type AssistantDiagnostic struct {
	Type      string             `json:"type"`
	Timestamp int64              `json:"timestamp"`
	Details   *DiagnosticDetails `json:"details,omitempty"`
}

// droppedThinkingCount is the number of thinking_dropped transformations on msg.
func droppedThinkingCount(msg *AssistantMessage) int {
	if msg == nil {
		return 0
	}
	n := 0
	for _, d := range msg.Diagnostics {
		if d.Type != "anthropic_input_transformations" || d.Details == nil {
			continue
		}
		for _, tr := range d.Details.Transformations {
			if tr.Type == "thinking_dropped" {
				n++
			}
		}
	}
	return n
}

// ThinkingDropNotice is the transcript line when current dropped more thinking
// blocks than previous. The same count stays quiet.
func ThinkingDropNotice(current, previous *AssistantMessage) string {
	dropped := droppedThinkingCount(current)
	if dropped == 0 || dropped <= droppedThinkingCount(previous) {
		return ""
	}
	noun := "thinking blocks"
	if dropped == 1 {
		noun = "thinking block"
	}
	return fmt.Sprintf("Anthropic dropped %d %s (details in session)", dropped, noun)
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
