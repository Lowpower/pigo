package ai

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/Lowpower/pigo/internal/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// BedrockClient talks to Amazon Bedrock ConverseStream.
type BedrockClient struct {
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

func withBearerHTTP(client *http.Client, token string) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	out := *client
	out.Transport = bearerRoundTripper{base: client.Transport, token: token}
	return &out
}

// StreamFn returns a StreamFn bound to this client.
func (c *BedrockClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		loadOpts := []func(*awsconfig.LoadOptions) error{}
		httpClient := c.HTTPClient
		if c.APIKey != "" {
			httpClient = withBearerHTTP(httpClient, c.APIKey)
		}
		if httpClient != nil {
			loadOpts = append(loadOpts, awsconfig.WithHTTPClient(httpClient))
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
		if err != nil {
			return errorStreamProvider(opts.Model, "bedrock-converse-stream", err.Error()), nil
		}
		client := bedrockruntime.NewFromConfig(awsCfg)
		input := &bedrockruntime.ConverseStreamInput{
			ModelId:  aws.String(opts.Model),
			Messages: bedrockMessages(reqCtx),
		}
		if reqCtx.System != "" {
			input.System = []types.SystemContentBlock{
				&types.SystemContentBlockMemberText{Value: reqCtx.System},
			}
		}
		if len(reqCtx.Tools) > 0 {
			tools := make([]types.Tool, 0, len(reqCtx.Tools))
			for _, t := range reqCtx.Tools {
				spec := types.ToolSpecification{Name: aws.String(t.Name), InputSchema: &types.ToolInputSchemaMemberJson{
					Value: document.NewLazyDocument(t.Parameters),
				}}
				if t.Description != "" {
					spec.Description = aws.String(t.Description)
				}
				tools = append(tools, &types.ToolMemberToolSpec{Value: spec})
			}
			input.ToolConfig = &types.ToolConfiguration{Tools: tools}
		}
		if opts.MaxTokens > 0 {
			input.InferenceConfig = &types.InferenceConfiguration{MaxTokens: aws.Int32(int32(opts.MaxTokens))}
		}
		if fields := bedrockThinkingFields(opts); fields != nil {
			input.AdditionalModelRequestFields = document.NewLazyDocument(fields)
		}

		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: "bedrock-converse-stream",
			Provider: "amazon-bedrock", Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			resp, err := client.ConverseStream(ctx, input)
			if err != nil {
				finishError(ctx, out, s, err.Error())
				return
			}
			processBedrockStream(ctx, resp.GetStream(), out, s)
		}()
		return s, nil
	}
}

func bedrockThinkingFields(opts Options) map[string]any {
	if reasoningEffort(opts) == "" && opts.ThinkingBudget <= 0 {
		return nil
	}
	budget := opts.ThinkingBudget
	if budget <= 0 {
		budget = models.BudgetTokens(opts.Thinking)
	}
	if budget > 0 {
		return map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": budget}}
	}
	return map[string]any{"thinking": map[string]any{"type": "adaptive"}}
}

func bedrockMessages(reqCtx Context) []types.Message {
	var out []types.Message
	for _, m := range reqCtx.Messages {
		if m.Assistant != nil {
			var blocks []types.ContentBlock
			for _, c := range m.Assistant.Content {
				switch c.Type {
				case KindText:
					blocks = append(blocks, &types.ContentBlockMemberText{Value: c.Text})
				case KindThinking:
					blocks = append(blocks, bedrockReasoningBlock(c))
				case KindToolCall:
					blocks = append(blocks, &types.ContentBlockMemberToolUse{Value: types.ToolUseBlock{
						ToolUseId: aws.String(c.ToolID),
						Name:      aws.String(c.ToolName),
						Input:     document.NewLazyDocument(c.Arguments),
					}})
				}
			}
			out = append(out, types.Message{Role: types.ConversationRoleAssistant, Content: blocks})
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			content := []types.ToolResultContentBlock{&types.ToolResultContentBlockMemberText{Value: m.Content}}
			for _, img := range m.Images {
				if b := bedrockImageBlock(img); b != nil {
					content = append(content, &types.ToolResultContentBlockMemberImage{Value: b.Value})
				}
			}
			out = append(out, types.Message{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{&types.ContentBlockMemberToolResult{Value: types.ToolResultBlock{
					ToolUseId: aws.String(m.ToolCallID),
					Content:   content,
				}}},
			})
			continue
		}
		role := types.ConversationRoleUser
		if m.Role == RoleAssistant {
			role = types.ConversationRoleAssistant
		}
		blocks := []types.ContentBlock{}
		if m.Content != "" {
			blocks = append(blocks, &types.ContentBlockMemberText{Value: m.Content})
		}
		for _, img := range m.Images {
			if b := bedrockImageBlock(img); b != nil {
				blocks = append(blocks, b)
			}
		}
		if len(blocks) == 0 {
			blocks = []types.ContentBlock{&types.ContentBlockMemberText{Value: m.Content}}
		}
		out = append(out, types.Message{Role: role, Content: blocks})
	}
	return out
}

func bedrockReasoningBlock(c *Content) types.ContentBlock {
	if c.Redacted {
		data, err := base64.StdEncoding.DecodeString(c.ThinkingSignature)
		if err != nil {
			data = []byte(c.ThinkingSignature)
		}
		return &types.ContentBlockMemberReasoningContent{
			Value: &types.ReasoningContentBlockMemberRedactedContent{Value: data},
		}
	}
	text := types.ReasoningTextBlock{Text: aws.String(c.Thinking)}
	if c.ThinkingSignature != "" {
		text.Signature = aws.String(c.ThinkingSignature)
	}
	return &types.ContentBlockMemberReasoningContent{
		Value: &types.ReasoningContentBlockMemberReasoningText{Value: text},
	}
}

func bedrockImageBlock(img ImageContent) *types.ContentBlockMemberImage {
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return nil
	}
	format := types.ImageFormatPng
	switch strings.ToLower(img.MimeType) {
	case "image/jpeg", "image/jpg":
		format = types.ImageFormatJpeg
	case "image/gif":
		format = types.ImageFormatGif
	case "image/webp":
		format = types.ImageFormatWebp
	}
	return &types.ContentBlockMemberImage{Value: types.ImageBlock{
		Format: format,
		Source: &types.ImageSourceMemberBytes{Value: data},
	}}
}

func processBedrockStream(ctx context.Context, stream *bedrockruntime.ConverseStreamEventStream, out *AssistantMessage, s *EventStream) {
	defer func() { _ = stream.Close() }()
	textIdx := map[int32]int{}
	thinkIdx := map[int32]int{}
	toolIdx := map[int32]int{}
	for ev := range stream.Events() {
		switch v := ev.(type) {
		case *types.ConverseStreamOutputMemberContentBlockStart:
			idx := int32(0)
			if v.Value.ContentBlockIndex != nil {
				idx = *v.Value.ContentBlockIndex
			}
			if start, ok := v.Value.Start.(*types.ContentBlockStartMemberToolUse); ok {
				out.Content = append(out.Content, &Content{
					Type: KindToolCall, ToolID: aws.ToString(start.Value.ToolUseId), ToolName: aws.ToString(start.Value.Name),
				})
				pos := len(out.Content) - 1
				toolIdx[idx] = pos
				if !s.push(ctx, Event{Type: EventToolCallStart, ContentIndex: pos, Partial: out}) {
					return
				}
			}
		case *types.ConverseStreamOutputMemberContentBlockDelta:
			idx := int32(0)
			if v.Value.ContentBlockIndex != nil {
				idx = *v.Value.ContentBlockIndex
			}
			switch d := v.Value.Delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				pos, ok := textIdx[idx]
				if !ok {
					out.Content = append(out.Content, &Content{Type: KindText})
					pos = len(out.Content) - 1
					textIdx[idx] = pos
					if !s.push(ctx, Event{Type: EventTextStart, ContentIndex: pos, Partial: out}) {
						return
					}
				}
				out.Content[pos].Text += d.Value
				if !s.push(ctx, Event{Type: EventTextDelta, ContentIndex: pos, Delta: d.Value, Partial: out}) {
					return
				}
			case *types.ContentBlockDeltaMemberReasoningContent:
				pos, ok := thinkIdx[idx]
				if !ok {
					out.Content = append(out.Content, &Content{Type: KindThinking})
					pos = len(out.Content) - 1
					thinkIdx[idx] = pos
					if !s.push(ctx, Event{Type: EventThinkingStart, ContentIndex: pos, Partial: out}) {
						return
					}
				}
				block := out.Content[pos]
				switch rd := d.Value.(type) {
				case *types.ReasoningContentBlockDeltaMemberText:
					block.Thinking += rd.Value
					if !s.push(ctx, Event{Type: EventThinkingDelta, ContentIndex: pos, Delta: rd.Value, Partial: out}) {
						return
					}
				case *types.ReasoningContentBlockDeltaMemberSignature:
					block.ThinkingSignature += rd.Value
				case *types.ReasoningContentBlockDeltaMemberRedactedContent:
					block.Redacted = true
					block.ThinkingSignature = base64.StdEncoding.EncodeToString(rd.Value)
					if block.Thinking == "" {
						block.Thinking = "[Reasoning redacted]"
					}
				}
			case *types.ContentBlockDeltaMemberToolUse:
				pos, ok := toolIdx[idx]
				if !ok {
					continue
				}
				if d.Value.Input != nil {
					block := out.Content[pos]
					block.partialJSON += *d.Value.Input
					block.Arguments = parseStreamingJSON(block.partialJSON)
					if !s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: pos, Delta: *d.Value.Input, Partial: out}) {
						return
					}
				}
			}
		case *types.ConverseStreamOutputMemberMessageStop:
			switch v.Value.StopReason {
			case types.StopReasonToolUse:
				out.StopReason = StopToolUse
			case types.StopReasonMaxTokens:
				out.StopReason = StopLength
			default:
				out.StopReason = StopStop
			}
		case *types.ConverseStreamOutputMemberMetadata:
			if v.Value.Usage != nil {
				if v.Value.Usage.InputTokens != nil {
					out.Usage.Input = int(*v.Value.Usage.InputTokens)
				}
				if v.Value.Usage.OutputTokens != nil {
					out.Usage.Output = int(*v.Value.Usage.OutputTokens)
				}
				out.Usage.TotalTokens = out.Usage.Input + out.Usage.Output
			}
		}
	}
	if err := stream.Err(); err != nil && out.StopReason != StopError {
		finishError(ctx, out, s, err.Error())
		return
	}
	for i, c := range out.Content {
		switch c.Type {
		case KindText:
			s.push(ctx, Event{Type: EventTextEnd, ContentIndex: i, Content: c.Text, Partial: out})
		case KindThinking:
			s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: i, Content: c.Thinking, Partial: out})
		case KindToolCall:
			if c.partialJSON != "" {
				c.Arguments = parseStreamingJSON(c.partialJSON)
				c.partialJSON = ""
			}
			s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: i, ToolCall: c, Partial: out})
		}
	}
	if out.StopReason == StopPending {
		if len(out.ToolCalls()) > 0 {
			out.StopReason = StopToolUse
		} else {
			out.StopReason = StopStop
		}
	}
	if out.StopReason == StopError {
		finishError(ctx, out, s, out.ErrorMessage)
		return
	}
	s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
}
