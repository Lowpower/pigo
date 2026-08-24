package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/Lowpower/pigo/internal/ai"
)

// Tool execution modes (pi AgentLoopConfig.toolExecution).
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
// themselves are implemented in Phase 3; the loop is tool-agnostic.
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
}

// Run drives the agent loop and returns a stream of AgentEvents. The loop runs
// turns until the model stops requesting tools (or MaxTurns is reached), matching
// pi's runLoop turn cycle. reqCtx.Messages seed the transcript; exec runs tool
// calls (may be nil if no tools are offered).
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
	emit := func(ev Event) bool { return s.push(ctx, ev) }

	// Seed the transcript from the request's messages.
	transcript := make([]Msg, 0, len(reqCtx.Messages)+4)
	for _, m := range reqCtx.Messages {
		transcript = append(transcript, Msg{Role: m.Role, Text: m.Content})
	}

	if !emit(Event{Type: EventAgentStart}) {
		return
	}

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		if !emit(Event{Type: EventTurnStart}) {
			return
		}

		aiCtx := ai.Context{System: reqCtx.System, Messages: toAIMessages(transcript), Tools: reqCtx.Tools}
		message, ok := streamAssistant(ctx, sf, aiCtx, cfg, s)
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
		if len(toolCalls) > 0 && message.StopReason != ai.StopLength {
			toolResults, ok = executeToolCalls(ctx, toolCalls, exec, cfg, s)
			if !ok {
				return
			}
			transcript = append(transcript, toolResults...)
		}

		if !emit(Event{Type: EventTurnEnd, Assistant: message, ToolResults: toolResults}) {
			return
		}

		// Stop when the model produced no (executable) tool calls this turn.
		if len(toolResults) == 0 {
			break
		}
	}

	emit(Event{Type: EventAgentEnd, Messages: transcript})
}

// streamAssistant consumes one provider turn, forwarding message_start/update/end
// events and returning the final assistant message. ok is false if the context
// was cancelled while emitting.
func streamAssistant(ctx context.Context, sf ai.StreamFn, aiCtx ai.Context, cfg Config, s *Stream) (*ai.AssistantMessage, bool) {
	stream, err := sf(ctx, aiCtx, ai.Options{Model: cfg.Model})
	if err != nil {
		msg := &ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopError, ErrorMessage: err.Error()}
		if !s.push(ctx, Event{Type: EventMessageEnd, Assistant: msg}) {
			return nil, false
		}
		return msg, true
	}

	started := false
	var final *ai.AssistantMessage
	for ev := range stream.Events() {
		if !started && ev.Partial != nil {
			if !s.push(ctx, Event{Type: EventMessageStart, Assistant: ev.Partial}) {
				return nil, false
			}
			started = true
		}
		evCopy := ev
		msg := ev.Partial
		if msg == nil {
			msg = ev.Message
		}
		if !s.push(ctx, Event{Type: EventMessageUpdate, Assistant: msg, AIEvent: &evCopy}) {
			return nil, false
		}
		if ev.Type == ai.EventDone || ev.Type == ai.EventError {
			final = ev.Message
		}
	}
	if final == nil {
		final = &ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopError, ErrorMessage: "provider stream ended without a terminal event"}
	}
	if !s.push(ctx, Event{Type: EventMessageEnd, Assistant: final}) {
		return nil, false
	}
	return final, true
}

// executeToolCalls runs the tool calls sequentially or in parallel. Tool-result
// messages are returned in the model's source order regardless of completion
// order (matching pi). ok is false if the context was cancelled while emitting.
func executeToolCalls(ctx context.Context, calls []ToolCall, exec ToolExecutor, cfg Config, s *Stream) ([]Msg, bool) {
	results := make([]Msg, len(calls))

	run := func(i int) bool {
		c := calls[i]
		if !s.push(ctx, Event{Type: EventToolStart, ToolCallID: c.ID, ToolName: c.Name, Args: c.Args}) {
			return false
		}
		var (
			out     string
			isError bool
		)
		if exec != nil {
			out, isError = exec.Execute(ctx, c)
		} else {
			out, isError = fmt.Sprintf("no executor for tool %q", c.Name), true
		}
		results[i] = Msg{Role: RoleToolResult, ToolCallID: c.ID, ToolName: c.Name, Text: out, IsError: isError}
		return s.push(ctx, Event{Type: EventToolEnd, ToolCallID: c.ID, ToolName: c.Name, Result: out, IsError: isError})
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

func toToolCalls(m *ai.AssistantMessage) []ToolCall {
	var calls []ToolCall
	for _, c := range m.ToolCalls() {
		calls = append(calls, ToolCall{ID: c.ToolID, Name: c.ToolName, Args: c.Arguments})
	}
	return calls
}

func toAIMessages(transcript []Msg) []ai.Message {
	out := make([]ai.Message, 0, len(transcript))
	for _, m := range transcript {
		switch m.Role {
		case RoleAssistant:
			text := ""
			if m.Assistant != nil {
				text = m.Assistant.Text()
			}
			out = append(out, ai.Message{Role: ai.RoleAssistant, Content: text})
		case RoleToolResult:
			out = append(out, ai.Message{Role: "tool", Content: fmt.Sprintf("[tool %s] %s", m.ToolName, m.Text)})
		default:
			out = append(out, ai.Message{Role: ai.RoleUser, Content: m.Text})
		}
	}
	return out
}
