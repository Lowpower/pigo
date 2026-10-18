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

func (c *GoogleClient) outboundHeaders(opts Options) http.Header {
	hdr := make(http.Header, len(c.Headers)+2)
	for k, v := range c.Headers {
		hdr.Set(k, v)
	}
	applyOpenCodeSessionHeader(hdr, c.Headers, opts)
	setDefaultUserAgent(hdr)
	applyExtraHeaders(hdr, opts.ExtraHeaders)
	return hdr
}

func (c *GoogleClient) genaiConfig() *genai.ClientConfig {
	return c.clientConfig(Options{})
}

func (c *GoogleClient) clientConfig(opts Options) *genai.ClientConfig {
	cfg := &genai.ClientConfig{HTTPClient: c.HTTPClient}
	httpOpts := genai.HTTPOptions{Headers: c.outboundHeaders(opts)}
	if c.BaseURL != "" {
		httpOpts.BaseURL = c.BaseURL
	}
	cfg.HTTPOptions = httpOpts
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
		clientCfg := c.clientConfig(opts)
		client, err := genai.NewClient(ctx, clientCfg)
		if err != nil {
			return errorStreamProvider(opts.Model, c.apiID(), err.Error()), nil
		}
		target := replayTarget{
			Provider: recordedProvider(opts, c.providerID()),
			API:      c.apiID(),
			Model:    opts.Model,
		}
		contents := googleContents(reqCtx, target)
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
			Provider: target.Provider, Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			textIdx, thinkIdx := -1, -1
			cost := catalogCost(opts)
			var finish genai.FinishReason
			var finishMessage string
			for resp, err := range client.Models.GenerateContentStream(ctx, opts.Model, contents, gcfg) {
				if err != nil {
					finishError(ctx, out, s, err.Error())
					return
				}
				if opts.OnProviderStreamEvent != nil && resp != nil {
					// SDKHTTPResponse carries response headers, which are not part of the event.
					saved := resp.SDKHTTPResponse
					resp.SDKHTTPResponse = nil
					observeSDKEvent(opts.OnProviderStreamEvent, resp)
					resp.SDKHTTPResponse = saved
				}
				if resp == nil || len(resp.Candidates) == 0 {
					continue
				}
				cand := resp.Candidates[0]
				if cand.FinishReason != "" {
					finish = cand.FinishReason
					// The Gemini API converter drops finishMessage. Vertex keeps the candidate field.
					finishMessage = cand.FinishMessage
				}
				if cand.Content == nil {
					if resp.UsageMetadata != nil {
						applyGoogleUsage(&out.Usage, resp.UsageMetadata)
						calculateCost(cost, &out.Usage)
					}
					continue
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
					applyGoogleUsage(&out.Usage, resp.UsageMetadata)
					calculateCost(cost, &out.Usage)
				}
			}
			if thinkIdx >= 0 {
				s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: thinkIdx, Content: out.Content[thinkIdx].Thinking, Partial: out})
			}
			if textIdx >= 0 {
				s.push(ctx, Event{Type: EventTextEnd, ContentIndex: textIdx, Content: out.Content[textIdx].Text, Partial: out})
			}
			if out.StopReason == StopPending {
				out.RawStopReason = string(finish)
				out.StopReason, out.ErrorMessage = mapGoogleFinishReason(finish, finishMessage, len(out.ToolCalls()) > 0)
			}
			if out.StopReason == StopError {
				finishError(ctx, out, s, out.ErrorMessage)
				return
			}
			s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
		}()
		return s, nil
	}
}

func googleThinkingConfig(opts Options) *genai.ThinkingConfig {
	level := googleThinkingWire(opts)
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

func mapGoogleFinishReason(reason genai.FinishReason, finishMessage string, toolUse bool) (StopReason, string) {
	switch reason {
	case genai.FinishReasonStop:
		if toolUse {
			return StopToolUse, ""
		}
		return StopStop, ""
	case genai.FinishReasonMaxTokens:
		return StopLength, ""
	default:
		label := string(reason)
		if label == "" {
			label = string(genai.FinishReasonUnspecified)
		}
		if finishMessage != "" {
			return StopError, label + ": " + finishMessage
		}
		return StopError, label
	}
}

func googleContents(reqCtx Context, target replayTarget) []*genai.Content {
	var out []*genai.Content
	// Gemini < 3 cannot nest images in functionResponse.parts. Those image
	// turns are held until the current run of function responses ends, so a
	// parallel batch stays in one user content.
	var pendingImages []*genai.Content
	flushToolImages := func() {
		if len(pendingImages) == 0 {
			return
		}
		out = append(out, pendingImages...)
		pendingImages = nil
	}
	msgs := transformMessages(reqCtx.Messages, target, func(id string, _ *AssistantMessage) string {
		return normalizeGoogleToolCallID(id, target.Model)
	})
	for _, m := range msgs {
		if m.Assistant != nil {
			flushToolImages()
			out = append(out, googleAssistantContent(m.Assistant, target))
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			part, imageTurn := googleToolResultPart(m, target)
			if imageTurn != nil {
				pendingImages = append(pendingImages, imageTurn)
			}
			if last := googleLastFunctionResponseContent(out); last != nil {
				last.Parts = append(last.Parts, part)
			} else {
				out = append(out, &genai.Content{Role: string(genai.RoleUser), Parts: []*genai.Part{part}})
			}
			continue
		}
		flushToolImages()
		out = append(out, googlePlainContent(m))
	}
	flushToolImages()
	return out
}

func googleAssistantContent(m *AssistantMessage, target replayTarget) *genai.Content {
	includeID := requiresGoogleToolCallID(target.Model)
	var parts []*genai.Part
	for _, c := range m.Content {
		if c == nil {
			continue
		}
		switch c.Type {
		case KindText:
			parts = append(parts, &genai.Part{Text: c.Text, ThoughtSignature: []byte(c.TextSignature)})
		case KindThinking:
			parts = append(parts, &genai.Part{Text: c.Thinking, Thought: true, ThoughtSignature: []byte(c.ThinkingSignature)})
		case KindToolCall:
			call := &genai.FunctionCall{Name: c.ToolName, Args: c.Arguments}
			if includeID {
				call.ID = c.ToolID
			}
			parts = append(parts, &genai.Part{FunctionCall: call, ThoughtSignature: []byte(c.ThinkingSignature)})
		}
	}
	return &genai.Content{Role: string(genai.RoleModel), Parts: parts}
}

func googleToolResultPart(m Message, target replayTarget) (*genai.Part, *genai.Content) {
	text, imgs := googleToolResultBody(m)
	if text == "" && len(imgs) > 0 {
		text = "(see attached image)"
	}
	key := "output"
	if m.IsError {
		key = "error"
	}
	fr := &genai.FunctionResponse{Name: m.ToolName, Response: map[string]any{key: text}}
	if requiresGoogleToolCallID(target.Model) {
		fr.ID = m.ToolCallID
	}
	var imageTurn *genai.Content
	if len(imgs) > 0 {
		if supportsGoogleMultimodalFunctionResponse(target.Model) {
			fr.Parts = googleFunctionResponseParts(imgs)
		} else {
			imageTurn = googleToolImageTurn(imgs)
		}
	}
	return &genai.Part{FunctionResponse: fr}, imageTurn
}

func googleToolResultBody(m Message) (string, []ImageContent) {
	text, imgs := ParseToolContent(m.Content)
	if len(imgs) == 0 && len(m.Images) > 0 {
		return m.Content, m.Images
	}
	return text, imgs
}

// supportsGoogleMultimodalFunctionResponse reports whether tool images belong
// inside functionResponse.parts. Gemini before 3 needs a separate user turn.
// Models that are not Gemini (Cloud Code Assist Claude, gpt-oss) follow pi
// and keep the images nested.
func supportsGoogleMultimodalFunctionResponse(modelID string) bool {
	major, ok := geminiMajorVersion(modelID)
	if !ok {
		return true
	}
	return major >= 3
}

func googleFunctionResponseParts(imgs []ImageContent) []*genai.FunctionResponsePart {
	parts := make([]*genai.FunctionResponsePart, 0, len(imgs))
	for _, img := range imgs {
		parts = append(parts, genai.NewFunctionResponsePartFromBytes(decodeImageData(img.Data), img.MimeType))
	}
	return parts
}

func googleToolImageTurn(imgs []ImageContent) *genai.Content {
	parts := make([]*genai.Part, 0, 1+len(imgs))
	parts = append(parts, &genai.Part{Text: "Tool result image:"})
	for _, img := range imgs {
		parts = append(parts, &genai.Part{InlineData: &genai.Blob{MIMEType: img.MimeType, Data: decodeImageData(img.Data)}})
	}
	return &genai.Content{Role: string(genai.RoleUser), Parts: parts}
}

func googleLastFunctionResponseContent(contents []*genai.Content) *genai.Content {
	if len(contents) == 0 {
		return nil
	}
	last := contents[len(contents)-1]
	if last == nil || last.Role != string(genai.RoleUser) {
		return nil
	}
	for _, part := range last.Parts {
		if part != nil && part.FunctionResponse != nil {
			return last
		}
	}
	return nil
}

func googlePlainContent(m Message) *genai.Content {
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
	return &genai.Content{Role: string(role), Parts: parts}
}

// applyGoogleUsage splits Gemini usage. PromptTokenCount includes cached
// content. Thoughts and tool-use tokens stay out of Output.
func applyGoogleUsage(dst *Usage, meta *genai.GenerateContentResponseUsageMetadata) {
	if dst == nil || meta == nil {
		return
	}
	applyInclusivePromptUsage(dst, int(meta.PromptTokenCount), int(meta.CandidatesTokenCount), int(meta.CachedContentTokenCount), 0)
}

func googleImageParts(m Message) []*genai.Part {
	imgs := m.Images
	if len(imgs) == 0 {
		_, parsed := ParseToolContent(m.Content)
		imgs = parsed
	}
	var parts []*genai.Part
	for _, img := range imgs {
		parts = append(parts, &genai.Part{InlineData: &genai.Blob{MIMEType: img.MimeType, Data: decodeImageData(img.Data)}})
	}
	return parts
}

func decodeImageData(data string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return []byte(data)
	}
	return decoded
}
