package ai

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/models"
)

// EmitMessage streams a canned AssistantMessage as a proper event sequence
// (start, per-block start/delta/end, done or error). It is used by the agent
// loop's tests to script provider turns (including tool-calling turns) without a
// live provider.
func EmitMessage(ctx context.Context, msg *AssistantMessage) *EventStream {
	s := NewEventStream(16)
	go func() {
		defer s.end()
		if !s.push(ctx, Event{Type: EventStart, Partial: msg}) {
			return
		}
		for i, c := range msg.Content {
			switch c.Type {
			case KindText:
				if !s.push(ctx, Event{Type: EventTextStart, ContentIndex: i, Partial: msg}) {
					return
				}
				if c.Text != "" && !s.push(ctx, Event{Type: EventTextDelta, ContentIndex: i, Delta: c.Text, Partial: msg}) {
					return
				}
				if !s.push(ctx, Event{Type: EventTextEnd, ContentIndex: i, Content: c.Text, Partial: msg}) {
					return
				}
			case KindThinking:
				if !s.push(ctx, Event{Type: EventThinkingStart, ContentIndex: i, Partial: msg}) {
					return
				}
				if !s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: i, Content: c.Thinking, Partial: msg}) {
					return
				}
			case KindToolCall:
				if !s.push(ctx, Event{Type: EventToolCallStart, ContentIndex: i, Partial: msg}) {
					return
				}
				if len(c.Arguments) > 0 {
					if b, err := json.Marshal(c.Arguments); err == nil {
						if !s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: i, Delta: string(b), Partial: msg}) {
							return
						}
					}
				}
				if !s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: i, ToolCall: c, Partial: msg}) {
					return
				}
			}
		}
		if msg.StopReason == StopError || msg.StopReason == StopAborted {
			s.push(ctx, Event{Type: EventError, Reason: msg.StopReason, Message: msg})
			return
		}
		reason := msg.StopReason
		if reason == "" || reason == StopPending {
			reason = StopStop
		}
		s.push(ctx, Event{Type: EventDone, Reason: reason, Message: msg})
	}()
	return s
}

// ScriptedStreamFn returns a StreamFn that ignores providers and streams a fixed
// reply, emitting a realistic event sequence (start, text_start, one text_delta
// per word, text_end, done). It is the offline stand-in used when no provider
// credentials are configured, so the send→stream-to-screen pipeline can be
// demonstrated and tested without network access.
func ScriptedStreamFn(reply string, wordDelay time.Duration) StreamFn {
	return func(ctx context.Context, _ Context, opts Options) (*EventStream, error) {
		s := NewEventStream(16)
		out := &AssistantMessage{
			Role:       RoleAssistant,
			Content:    []*Content{},
			API:        "mock",
			Provider:   "mock",
			Model:      opts.Model,
			StopReason: StopPending,
		}
		go func() {
			defer s.end()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			block := &Content{Type: KindText}
			out.Content = append(out.Content, block)
			if !s.push(ctx, Event{Type: EventTextStart, ContentIndex: 0, Partial: out}) {
				return
			}

			words := strings.Fields(reply)
			for i, w := range words {
				chunk := w
				if i < len(words)-1 {
					chunk += " "
				}
				block.Text += chunk
				if !s.push(ctx, Event{Type: EventTextDelta, ContentIndex: 0, Delta: chunk, Partial: out}) {
					return
				}
				if wordDelay > 0 {
					select {
					case <-time.After(wordDelay):
					case <-ctx.Done():
						return
					}
				}
			}

			if !s.push(ctx, Event{Type: EventTextEnd, ContentIndex: 0, Content: block.Text, Partial: out}) {
				return
			}
			out.StopReason = StopStop
			out.Usage.Output = len(words)
			out.Usage.TotalTokens = len(words)
			s.push(ctx, Event{Type: EventDone, Reason: StopStop, Message: out})
		}()
		return s, nil
	}
}

// EchoStreamFn is a mock StreamFn that streams back the last user message.
func EchoStreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		last := ""
		for i := len(reqCtx.Messages) - 1; i >= 0; i-- {
			if reqCtx.Messages[i].Role == RoleUser {
				last = reqCtx.Messages[i].Content
				break
			}
		}
		reply := "You said: " + last +
			"  (mock provider — set ANTHROPIC_API_KEY or OPENAI_API_KEY to use a real model.)"
		return ScriptedStreamFn(reply, 40*time.Millisecond)(ctx, reqCtx, opts)
	}
}

// DefaultStreamFn selects a provider from the environment and returns it with a
// display name. Priority matches models.PickInitial: OpenCode, Anthropic,
// OpenAI, Google, Bedrock, then the offline mock.
func DefaultStreamFn() (fn StreamFn, name string) {
	for _, id := range []string{"opencode", "anthropic", "openai", "google", "amazon-bedrock"} {
		spec, ok := models.LookupProvider(id)
		if !ok {
			continue
		}
		key := envKey(spec.Env...)
		if key == "" && id == "amazon-bedrock" && bedrockAmbient() {
			return StreamFor(id, ClientConfig{}), id
		}
		if key == "" {
			continue
		}
		base := spec.BaseURL
		switch id {
		case "anthropic":
			if v := os.Getenv("ANTHROPIC_BASE_URL"); v != "" {
				base = strings.TrimRight(v, "/")
			}
		case "openai":
			if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
				base = strings.TrimRight(v, "/")
			}
		case "opencode":
			if v := os.Getenv("OPENCODE_BASE_URL"); v != "" {
				base = strings.TrimRight(v, "/")
			}
		}
		return StreamFor(id, ClientConfig{APIKey: key, BaseURL: base}), id
	}
	return EchoStreamFn(), "mock"
}
