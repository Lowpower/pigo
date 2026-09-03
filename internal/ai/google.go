package ai

import (
	"context"
	"encoding/base64"
	"net/http"

	"google.golang.org/genai"
)

// GoogleClient talks to Gemini (google-generative-ai) or Vertex AI.
type GoogleClient struct {
	APIKey     string
	BaseURL    string
	Project    string
	Location   string
	Headers    map[string]string
	HTTPClient *http.Client
	Vertex     bool
}

func (c *GoogleClient) apiID() string {
	if c.Vertex {
		return "google-vertex"
	}
	return "google-generative-ai"
}

func (c *GoogleClient) providerID() string {
	if c.Vertex {
		return "google-vertex"
	}
	return "google"
}

func (c *GoogleClient) genaiConfig() *genai.ClientConfig {
	cfg := &genai.ClientConfig{HTTPClient: c.HTTPClient}
	if c.BaseURL != "" || len(c.Headers) > 0 {
		hdr := make(http.Header, len(c.Headers))
		for k, v := range c.Headers {
			hdr.Set(k, v)
		}
		cfg.HTTPOptions = genai.HTTPOptions{BaseURL: c.BaseURL, Headers: hdr}
	}
	if c.Vertex {
		cfg.Backend = genai.BackendVertexAI
		cfg.Project = c.Project
		cfg.Location = c.Location
		if c.APIKey != "" {
			cfg.APIKey = c.APIKey
		}
		return cfg
	}
	cfg.APIKey = c.APIKey
	cfg.Backend = genai.BackendGeminiAPI
	return cfg
}

func (c *GoogleClient) missingAuth() string {
	if c.Vertex {
		if c.APIKey != "" {
			return ""
		}
		if c.Project == "" || c.Location == "" {
			return "Vertex AI requires GOOGLE_CLOUD_API_KEY or project+location"
		}
		return ""
	}
	if c.APIKey == "" {
		return "no API key for provider: google"
	}
	return ""
}

// StreamFn returns a StreamFn bound to this client.
func (c *GoogleClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		if msg := c.missingAuth(); msg != "" {
			return errorStreamProvider(opts.Model, c.apiID(), msg), nil
		}
		client, err := genai.NewClient(ctx, c.genaiConfig())
		if err != nil {
			return errorStreamProvider(opts.Model, c.apiID(), err.Error()), nil
		}
		contents := googleContents(reqCtx)
		gcfg := &genai.GenerateContentConfig{}
		if reqCtx.System != "" {
			gcfg.SystemInstruction = genai.NewContentFromText(reqCtx.System, genai.RoleUser)
		}
		if opts.MaxTokens > 0 {
			gcfg.MaxOutputTokens = int32(opts.MaxTokens)
		}
		if tc := googleThinkingConfig(opts); tc != nil {
			gcfg.ThinkingConfig = tc
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
			Role: RoleAssistant, Content: []*Content{}, API: c.apiID(),
			Provider: c.providerID(), Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			textIdx, thinkIdx := -1, -1
			var finish genai.FinishReason
			for resp, err := range client.Models.GenerateContentStream(ctx, opts.Model, contents, gcfg) {
				if err != nil {
					finishError(ctx, out, s, err.Error())
					return
				}
				if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
					continue
				}
				cand := resp.Candidates[0]
				if cand.FinishReason != "" {
					finish = cand.FinishReason
				}
				for _, p := range cand.Content.Parts {
					if p == nil {
						continue
					}
					if p.Thought && (p.Text != "" || len(p.ThoughtSignature) > 0) {
						if thinkIdx < 0 {
							out.Content = append(out.Content, &Content{Type: KindThinking})
							thinkIdx = len(out.Content) - 1
							if !s.push(ctx, Event{Type: EventThinkingStart, ContentIndex: thinkIdx, Partial: out}) {
								return
							}
						}
						if len(p.ThoughtSignature) > 0 {
							out.Content[thinkIdx].ThinkingSignature = string(p.ThoughtSignature)
						}
						if p.Text != "" {
							out.Content[thinkIdx].Thinking += p.Text
							if !s.push(ctx, Event{Type: EventThinkingDelta, ContentIndex: thinkIdx, Delta: p.Text, Partial: out}) {
								return
							}
						}
						continue
					}
					if p.FunctionCall != nil {
						block := &Content{
							Type: KindToolCall, ToolID: firstNonEmpty(p.FunctionCall.ID, p.FunctionCall.Name),
							ToolName: p.FunctionCall.Name, Arguments: p.FunctionCall.Args,
							ThinkingSignature: string(p.ThoughtSignature),
						}
						out.Content = append(out.Content, block)
						idx := len(out.Content) - 1
						if !s.push(ctx, Event{Type: EventToolCallStart, ContentIndex: idx, Partial: out}) {
							return
						}
						if !s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: idx, ToolCall: out.Content[idx], Partial: out}) {
							return
						}
						continue
					}
					if p.Text == "" && len(p.ThoughtSignature) == 0 {
						continue
					}
					if textIdx < 0 {
						out.Content = append(out.Content, &Content{Type: KindText})
						textIdx = len(out.Content) - 1
						if !s.push(ctx, Event{Type: EventTextStart, ContentIndex: textIdx, Partial: out}) {
							return
						}
					}
					if len(p.ThoughtSignature) > 0 {
						out.Content[textIdx].TextSignature = string(p.ThoughtSignature)
					}
					if p.Text == "" {
						continue
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
				out.StopReason = mapGoogleFinishReason(finish, len(out.ToolCalls()) > 0)
			}
			s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
		}()
		return s, nil
	}
}

func googleThinkingConfig(opts Options) *genai.ThinkingConfig {
	level := googleThinkingLevel(opts.Thinking)
	if opts.ThinkingBudget <= 0 && level == "" {
		return nil
	}
	cfg := &genai.ThinkingConfig{IncludeThoughts: true}
	if opts.ThinkingBudget > 0 {
		b := int32(opts.ThinkingBudget)
		cfg.ThinkingBudget = &b
	}
	if level != "" {
		cfg.ThinkingLevel = genai.ThinkingLevel(level)
	}
	return cfg
}

func mapGoogleFinishReason(reason genai.FinishReason, toolUse bool) StopReason {
	switch reason {
	case genai.FinishReasonMaxTokens:
		return StopLength
	case genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonBlocklist, genai.FinishReasonProhibitedContent:
		return StopError
	case genai.FinishReasonStop, "":
		if toolUse {
			return StopToolUse
		}
		return StopStop
	default:
		if toolUse {
			return StopToolUse
		}
		return StopStop
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
					parts = append(parts, &genai.Part{Text: c.Text, ThoughtSignature: []byte(c.TextSignature)})
				case KindThinking:
					parts = append(parts, &genai.Part{Text: c.Thinking, Thought: true, ThoughtSignature: []byte(c.ThinkingSignature)})
				case KindToolCall:
					parts = append(parts, &genai.Part{
						FunctionCall:     &genai.FunctionCall{ID: c.ToolID, Name: c.ToolName, Args: c.Arguments},
						ThoughtSignature: []byte(c.ThinkingSignature),
					})
				}
			}
			out = append(out, &genai.Content{Role: string(genai.RoleModel), Parts: parts})
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			parts := []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{ID: m.ToolCallID, Name: m.ToolName, Response: map[string]any{"output": m.Content}},
			}}
			parts = append(parts, googleImageParts(m)...)
			out = append(out, &genai.Content{Role: string(genai.RoleUser), Parts: parts})
			continue
		}
		var role genai.Role = genai.RoleUser
		if m.Role == RoleAssistant {
			role = genai.RoleModel
		}
		parts := []*genai.Part{}
		if m.Content != "" {
			parts = append(parts, &genai.Part{Text: m.Content})
		}
		parts = append(parts, googleImageParts(m)...)
		if len(parts) == 0 {
			parts = []*genai.Part{{Text: m.Content}}
		}
		out = append(out, &genai.Content{Role: string(role), Parts: parts})
	}
	return out
}

func googleImageParts(m Message) []*genai.Part {
	imgs := m.Images
	if len(imgs) == 0 {
		_, parsed := ParseToolContent(m.Content)
		imgs = parsed
	}
	var parts []*genai.Part
	for _, img := range imgs {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			data = []byte(img.Data)
		}
		parts = append(parts, &genai.Part{InlineData: &genai.Blob{MIMEType: img.MimeType, Data: data}})
	}
	return parts
}
