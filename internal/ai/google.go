package ai

import (
	"context"
	"net/http"

	"google.golang.org/genai"
)

// GoogleClient talks to Gemini (google-generative-ai).
type GoogleClient struct {
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

// StreamFn returns a StreamFn bound to this client.
func (c *GoogleClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		if c.APIKey == "" {
			return errorStreamProvider(opts.Model, "google-generative-ai", "no API key for provider: google"), nil
		}
		cfg := &genai.ClientConfig{APIKey: c.APIKey, Backend: genai.BackendGeminiAPI, HTTPClient: c.HTTPClient}
		client, err := genai.NewClient(ctx, cfg)
		if err != nil {
			return errorStreamProvider(opts.Model, "google-generative-ai", err.Error()), nil
		}
		contents := googleContents(reqCtx)
		gcfg := &genai.GenerateContentConfig{}
		if reqCtx.System != "" {
			gcfg.SystemInstruction = genai.NewContentFromText(reqCtx.System, genai.RoleUser)
		}
		if opts.MaxTokens > 0 {
			gcfg.MaxOutputTokens = int32(opts.MaxTokens)
		}
		if opts.ThinkingBudget > 0 {
			b := int32(opts.ThinkingBudget)
			gcfg.ThinkingConfig = &genai.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: &b}
		}
		if len(reqCtx.Tools) > 0 {
			decls := make([]*genai.FunctionDeclaration, 0, len(reqCtx.Tools))
			for _, t := range reqCtx.Tools {
				decls = append(decls, &genai.FunctionDeclaration{
					Name: t.Name, Description: t.Description, ParametersJsonSchema: t.Parameters,
				})
			}
			gcfg.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
		}
		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: "google-generative-ai",
			Provider: "google", Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			textIdx, thinkIdx := -1, -1
			for resp, err := range client.Models.GenerateContentStream(ctx, opts.Model, contents, gcfg) {
				if err != nil {
					finishError(ctx, out, s, err.Error())
					return
				}
				if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
					continue
				}
				for _, p := range resp.Candidates[0].Content.Parts {
					if p == nil {
						continue
					}
					if p.Thought && p.Text != "" {
						if thinkIdx < 0 {
							out.Content = append(out.Content, &Content{Type: KindThinking})
							thinkIdx = len(out.Content) - 1
							if !s.push(ctx, Event{Type: EventThinkingStart, ContentIndex: thinkIdx, Partial: out}) {
								return
							}
						}
						out.Content[thinkIdx].Thinking += p.Text
						if !s.push(ctx, Event{Type: EventThinkingDelta, ContentIndex: thinkIdx, Delta: p.Text, Partial: out}) {
							return
						}
						continue
					}
					if p.FunctionCall != nil {
						out.Content = append(out.Content, &Content{
							Type: KindToolCall, ToolID: firstNonEmpty(p.FunctionCall.ID, p.FunctionCall.Name),
							ToolName: p.FunctionCall.Name, Arguments: p.FunctionCall.Args,
						})
						idx := len(out.Content) - 1
						if !s.push(ctx, Event{Type: EventToolCallStart, ContentIndex: idx, Partial: out}) {
							return
						}
						if !s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: idx, ToolCall: out.Content[idx], Partial: out}) {
							return
						}
						continue
					}
					if p.Text == "" {
						continue
					}
					if textIdx < 0 {
						out.Content = append(out.Content, &Content{Type: KindText})
						textIdx = len(out.Content) - 1
						if !s.push(ctx, Event{Type: EventTextStart, ContentIndex: textIdx, Partial: out}) {
							return
						}
					}
					out.Content[textIdx].Text += p.Text
					if !s.push(ctx, Event{Type: EventTextDelta, ContentIndex: textIdx, Delta: p.Text, Partial: out}) {
						return
					}
				}
				if resp.UsageMetadata != nil {
					out.Usage.Input = int(resp.UsageMetadata.PromptTokenCount)
					out.Usage.Output = int(resp.UsageMetadata.CandidatesTokenCount)
					out.Usage.TotalTokens = int(resp.UsageMetadata.TotalTokenCount)
				}
			}
			if thinkIdx >= 0 {
				s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: thinkIdx, Content: out.Content[thinkIdx].Thinking, Partial: out})
			}
			if textIdx >= 0 {
				s.push(ctx, Event{Type: EventTextEnd, ContentIndex: textIdx, Content: out.Content[textIdx].Text, Partial: out})
			}
			if out.StopReason == StopPending {
				if len(out.ToolCalls()) > 0 {
					out.StopReason = StopToolUse
				} else {
					out.StopReason = StopStop
				}
			}
			s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
		}()
		return s, nil
	}
}

func googleContents(reqCtx Context) []*genai.Content {
	var out []*genai.Content
	for _, m := range reqCtx.Messages {
		if m.Assistant != nil {
			var parts []*genai.Part
			for _, c := range m.Assistant.Content {
				switch c.Type {
				case KindText:
					parts = append(parts, &genai.Part{Text: c.Text})
				case KindThinking:
					parts = append(parts, &genai.Part{Text: c.Thinking, Thought: true})
				case KindToolCall:
					parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: c.ToolID, Name: c.ToolName, Args: c.Arguments}})
				}
			}
			out = append(out, &genai.Content{Role: genai.RoleModel, Parts: parts})
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			out = append(out, &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{ID: m.ToolCallID, Name: m.ToolName, Response: map[string]any{"output": m.Content}},
			}}})
			continue
		}
		var role genai.Role = genai.RoleUser
		if m.Role == RoleAssistant {
			role = genai.RoleModel
		}
		out = append(out, genai.NewContentFromText(m.Content, role))
	}
	return out
}
