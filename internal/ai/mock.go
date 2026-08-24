package ai

import (
	"context"
	"strings"
	"time"
)

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
			"  (mock provider — set ANTHROPIC_API_KEY, or ANTHROPIC_BASE_URL + key for a gateway, to use a real model.)"
		return ScriptedStreamFn(reply, 40*time.Millisecond)(ctx, reqCtx, opts)
	}
}

// DefaultStreamFn returns the Anthropic StreamFn when ANTHROPIC_API_KEY is set,
// otherwise the mock EchoStreamFn. live reports whether a real provider is used.
func DefaultStreamFn() (fn StreamFn, live bool) {
	if c, ok := NewAnthropicFromEnv(); ok {
		return c.StreamFn(), true
	}
	return EchoStreamFn(), false
}
