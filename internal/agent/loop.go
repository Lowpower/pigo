package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/tools"
)

// Tool execution modes.
const (
	Sequential = "sequential"
	Parallel   = "parallel"
)

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolExecutor runs a tool call and returns its textual result. isError reports
// whether the tool failed (the result then describes the failure). Tools
// themselves live in internal/tools; the loop is tool-agnostic.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (result string, isError bool)
}

// ToolFunc adapts a function to ToolExecutor.
type ToolFunc func(ctx context.Context, call ToolCall) (string, bool)

// Execute implements ToolExecutor.
func (f ToolFunc) Execute(ctx context.Context, call ToolCall) (string, bool) {
	return f(ctx, call)
}

// Config controls the loop.
type Config struct {
	Model         string
	ToolExecution string // Sequential | Parallel (default Parallel)
	MaxTurns      int    // safety cap (default 16)
	Thinking      string // thinking level; forwarded to the provider
	// Steering is polled after every turn. Returned user messages are injected
	// before the next LLM call.
	Steering func() []ai.Message
	// FollowUp is polled when the model produced no tool calls.
	FollowUp func() []ai.Message
	// NewUserMessages are emitted as message_start/end after the first turn_start.
	// Historical context messages are not re-emitted.
	NewUserMessages []ai.Message
	SessionID       string
	Provider        string
	CacheRetention  string
	// PrepareNextTurn may rewrite the transcript after tool results, before
	// the next provider call (threshold compaction).
	PrepareNextTurn func(ctx context.Context, msgs []ai.Message) []ai.Message
	// OnLifecycle is invoked after lifecycle events are pushed, including
	// message_start/update/end and tool_execution_update.
	OnLifecycle func(Event)
	// OnMessageEnd may replace a finalized assistant message before it is
	// pushed as message_end and appended to the transcript.
	OnMessageEnd func(*ai.AssistantMessage) *ai.AssistantMessage
}

// Run drives the agent loop and returns a stream of AgentEvents. The loop runs
// turns until the model stops requesting tools (or MaxTurns is reached).
// reqCtx.Messages seed the transcript; exec runs tool calls (may be nil if no
// tools are offered).
func Run(ctx context.Context, sf ai.StreamFn, reqCtx ai.Context, exec ToolExecutor, cfg Config) *Stream {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 16
	}
	if cfg.ToolExecution == "" {
		cfg.ToolExecution = Parallel
	}
	s := newStream(64)
	go func() {
		defer s.end()
		runLoop(ctx, sf, reqCtx, exec, cfg, s)
	}()
	return s
}

func runLoop(ctx context.Context, sf ai.StreamFn, reqCtx ai.Context, exec ToolExecutor, cfg Config, s *Stream) {
	emit := func(ev Event) bool {
		ok := s.push(ctx, ev)
		if ok && cfg.OnLifecycle != nil {
			switch ev.Type {
			case EventAgentStart, EventAgentEnd, EventTurnStart, EventTurnEnd,
				EventToolStart, EventToolEnd, EventMessageStart, EventMessageUpdate,
				EventMessageEnd, EventToolUpdate:
				cfg.OnLifecycle(ev)
			}
		}
		return ok
	}

	// Seed the transcript from the request's messages, preserving assistant
	// tool-call blocks and toolResult pairing.
	transcript := make([]Msg, 0, len(reqCtx.Messages)+4)
	for _, m := range reqCtx.Messages {
		transcript = append(transcript, msgFromAI(m))
	}
	pending := drainQueue(cfg.Steering)

	if !emit(Event{Type: EventAgentStart}) {
		return
	}

	firstTurn := true
	for turn := 0; turn < cfg.MaxTurns; turn++ {
		if !emit(Event{Type: EventTurnStart}) {
			return
		}
		if firstTurn {
			for _, m := range cfg.NewUserMessages {
				msg := msgFromAI(m)
				if !emit(Event{Type: EventMessageStart, Msg: &msg}) {
					return
				}
				if !emit(Event{Type: EventMessageEnd, Msg: &msg}) {
					return
				}
			}
			firstTurn = false
		}
		if len(pending) > 0 {
			for _, m := range pending {
				msg := msgFromAI(m)
				if !emit(Event{Type: EventMessageStart, Msg: &msg}) {
					return
				}
				if !emit(Event{Type: EventMessageEnd, Msg: &msg}) {
					return
				}
				transcript = append(transcript, msg)
			}
		}

		aiCtx := ai.Context{System: reqCtx.System, Messages: toAIMessages(transcript), Tools: reqCtx.Tools}
		message, ok := streamAssistant(ctx, sf, aiCtx, cfg, emit)
		if !ok {
			return
		}
		transcript = append(transcript, Msg{Role: RoleAssistant, Assistant: message})

		if message.StopReason == ai.StopError || message.StopReason == ai.StopAborted {
			emit(Event{Type: EventTurnEnd, Assistant: message})
			emit(Event{Type: EventAgentEnd, Messages: transcript})
			return
		}

		toolCalls := toToolCalls(message)
		var toolResults []Msg
		// A length stop still materialises tool results as errors so the
		// next turn can continue; truncated calls are not executed.
		if len(toolCalls) > 0 {
			if message.StopReason == ai.StopLength {
				toolResults, ok = failToolCalls(toolCalls, emit)
			} else {
				toolResults, ok = executeToolCalls(ctx, toolCalls, exec, cfg, emit)
			}
			if !ok {
				return
			}
			transcript = append(transcript, toolResults...)
			for i := range toolResults {
				m := toolResults[i]
				if !emit(Event{Type: EventMessageStart, Msg: &m}) {
					return
				}
				if !emit(Event{Type: EventMessageEnd, Msg: &m}) {
					return
				}
			}
			if cfg.PrepareNextTurn != nil {
				next := cfg.PrepareNextTurn(ctx, toAIMessages(transcript))
				transcript = transcriptFromAI(next)
			}
		}

		if !emit(Event{Type: EventTurnEnd, Assistant: message, ToolResults: toolResults}) {
			return
		}

		pending = drainQueue(cfg.Steering)
		if len(toolResults) == 0 && len(pending) == 0 {
			pending = drainQueue(cfg.FollowUp)
			if len(pending) == 0 {
				break
			}
		}
	}

	emit(Event{Type: EventAgentEnd, Messages: transcript})
}

// streamAssistant consumes one provider turn, forwarding message_start/update/end
// events and returning the final assistant message. ok is false if the context
// was cancelled while emitting.
func streamAssistant(ctx context.Context, sf ai.StreamFn, aiCtx ai.Context, cfg Config, emit func(Event) bool) (*ai.AssistantMessage, bool) {
	stream, err := sf(ctx, aiCtx, ai.Options{
		Model:          cfg.Model,
		Thinking:       cfg.Thinking,
		SessionID:      cfg.SessionID,
		Provider:       cfg.Provider,
		CacheRetention: cfg.CacheRetention,
	})
	if err != nil {
		msg := &ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopError, ErrorMessage: err.Error()}
		return emitAssistantEnd(cfg, emit, msg)
	}

	started := false
	var final *ai.AssistantMessage
	for ev := range stream.Events() {
		if !started && ev.Partial != nil {
			if !emit(Event{Type: EventMessageStart, Assistant: ev.Partial}) {
				return nil, false
			}
			started = true
		}
		evCopy := ev
		msg := ev.Partial
		if msg == nil {
			msg = ev.Message
		}
		if !emit(Event{Type: EventMessageUpdate, Assistant: msg, AIEvent: &evCopy}) {
			return nil, false
		}
		if ev.Type == ai.EventDone || ev.Type == ai.EventError {
			final = ev.Message
		}
	}
	if final == nil {
		final = &ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopError, ErrorMessage: "provider stream ended without a terminal event"}
	}
	return emitAssistantEnd(cfg, emit, final)
}

func emitAssistantEnd(cfg Config, emit func(Event) bool, msg *ai.AssistantMessage) (*ai.AssistantMessage, bool) {
	if cfg.OnMessageEnd != nil {
		if next := cfg.OnMessageEnd(msg); next != nil {
			msg = next
		}
	}
	if !emit(Event{Type: EventMessageEnd, Assistant: msg}) {
		return nil, false
	}
	return msg, true
}

// executeToolCalls runs the tool calls sequentially or in parallel. Tool-result
// messages are returned in the model's source order regardless of completion
// order. ok is false if the context was cancelled while emitting.
func executeToolCalls(ctx context.Context, calls []ToolCall, exec ToolExecutor, cfg Config, emit func(Event) bool) ([]Msg, bool) {
	results := make([]Msg, len(calls))

	run := func(i int) bool {
		c := calls[i]
		if !emit(Event{Type: EventToolStart, ToolCallID: c.ID, ToolName: c.Name, Args: c.Args}) {
			return false
		}
		var (
			out     string
			isError bool
		)
		if exec != nil {
			toolCtx := tools.WithOutputUpdate(ctx, func(partial string) {
				_ = emit(Event{
					Type:       EventToolUpdate,
					ToolCallID: c.ID,
					ToolName:   c.Name,
					Args:       c.Args,
					Result:     partial,
				})
			})
			out, isError = exec.Execute(toolCtx, c)
		} else {
			out, isError = fmt.Sprintf("no executor for tool %q", c.Name), true
		}
		results[i] = Msg{Role: RoleToolResult, ToolCallID: c.ID, ToolName: c.Name, Text: out, IsError: isError}
		return emit(Event{Type: EventToolEnd, ToolCallID: c.ID, ToolName: c.Name, Result: out, IsError: isError})
	}

	if cfg.ToolExecution == Parallel && len(calls) > 1 {
		var wg sync.WaitGroup
		okFlags := make([]bool, len(calls))
		for i := range calls {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				okFlags[i] = run(i)
			}(i)
		}
		wg.Wait()
		for _, ok := range okFlags {
			if !ok {
				return nil, false
			}
		}
		return results, true
	}

	for i := range calls {
		if !run(i) {
			return nil, false
		}
	}
	return results, true
}

func failToolCalls(calls []ToolCall, emit func(Event) bool) ([]Msg, bool) {
	results := make([]Msg, len(calls))
	for i, c := range calls {
		if !emit(Event{Type: EventToolStart, ToolCallID: c.ID, ToolName: c.Name, Args: c.Args}) {
			return nil, false
		}
		out := "tool call aborted: assistant message truncated (stop reason length)"
		results[i] = Msg{Role: RoleToolResult, ToolCallID: c.ID, ToolName: c.Name, Text: out, IsError: true}
		if !emit(Event{Type: EventToolEnd, ToolCallID: c.ID, ToolName: c.Name, Result: out, IsError: true}) {
			return nil, false
		}
	}
	return results, true
}

func drainQueue(fn func() []ai.Message) []ai.Message {
	if fn == nil {
		return nil
	}
	return fn()
}

func msgFromAI(m ai.Message) Msg {
	switch {
	case m.Assistant != nil || m.Role == RoleAssistant:
		text := m.Content
		if m.Assistant != nil {
			if t := m.Assistant.Text(); t != "" {
				text = t
			}
		}
		return Msg{Role: RoleAssistant, Text: text, Assistant: m.Assistant}
	case m.Role == RoleToolResult || m.Role == ai.RoleToolResult || m.ToolCallID != "":
		return Msg{Role: RoleToolResult, Text: m.Content, ToolCallID: m.ToolCallID, ToolName: m.ToolName, IsError: m.IsError}
	default:
		return Msg{Role: RoleUser, Text: m.Content, Images: m.Images}
	}
}
func toToolCalls(m *ai.AssistantMessage) []ToolCall {
	var calls []ToolCall
	for _, c := range m.ToolCalls() {
		calls = append(calls, ToolCall{ID: c.ToolID, Name: c.ToolName, Args: c.Arguments})
	}
	return calls
}

func toAIMessages(transcript []Msg) []ai.Message {
	return MessagesFromTranscript(transcript)
}

// MessagesFromTranscript is the exported form of toAIMessages, used by the TUI
// to keep on-screen history aligned with the loop transcript.

// MessagesFromTranscript converts the agent transcript into provider-facing
// ai.Messages, preserving assistant tool-call blocks and toolResult pairing.
func MessagesFromTranscript(transcript []Msg) []ai.Message {
	out := make([]ai.Message, 0, len(transcript))
	for _, m := range transcript {
		switch m.Role {
		case RoleAssistant:
			text := ""
			if m.Assistant != nil {
				text = m.Assistant.Text()
			}
			out = append(out, ai.Message{
				Role:      ai.RoleAssistant,
				Content:   text,
				Assistant: m.Assistant,
			})
		case RoleToolResult:
			out = append(out, ai.Message{
				Role:       ai.RoleToolResult,
				Content:    m.Text,
				ToolCallID: m.ToolCallID,
				ToolName:   m.ToolName,
				IsError:    m.IsError,
			})
		default:
			out = append(out, ai.Message{Role: ai.RoleUser, Content: m.Text, Images: m.Images})
		}
	}
	return out
}

func transcriptFromAI(msgs []ai.Message) []Msg {
	out := make([]Msg, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, msgFromAI(m))
	}
	return out
}
