package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

// OpenAIResponsesClient talks to the OpenAI Responses API.
type OpenAIResponsesClient struct {
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

// StreamFn returns a StreamFn bound to this client.
func (c *OpenAIResponsesClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		base := strings.TrimRight(c.BaseURL, "/")
		if base == "" {
			base = "https://api.openai.com/v1"
		} else if !strings.HasSuffix(base, "/v1") && !strings.Contains(base, "/v1/") {
			base += "/v1"
		}
		optsList := []option.RequestOption{
			option.WithAPIKey(c.APIKey),
			option.WithBaseURL(base + "/"),
		}
		if c.HTTPClient != nil {
			optsList = append(optsList, option.WithHTTPClient(c.HTTPClient))
		}
		for k, v := range c.Headers {
			optsList = append(optsList, option.WithHeader(k, v))
		}
		client := openai.NewClient(optsList...)

		params := responses.ResponseNewParams{
			Model: shared.ResponsesModel(opts.Model),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: buildResponsesInput(reqCtx),
			},
		}
		if reqCtx.System != "" {
			params.Instructions = param.NewOpt(reqCtx.System)
		}
		if opts.MaxTokens > 0 {
			n := int64(opts.MaxTokens)
			if n < 16 {
				n = 16
			}
			params.MaxOutputTokens = param.NewOpt(n)
		}
		if len(reqCtx.Tools) > 0 {
			tools := make([]responses.ToolUnionParam, 0, len(reqCtx.Tools))
			for _, t := range reqCtx.Tools {
				ft := responses.FunctionToolParam{
					Name:       t.Name,
					Parameters: t.Parameters,
					Strict:     param.NewOpt(false),
				}
				if t.Description != "" {
					ft.Description = param.NewOpt(t.Description)
				}
				tools = append(tools, responses.ToolUnionParam{OfFunction: &ft})
			}
			params.Tools = tools
		}

		stream := client.Responses.NewStreaming(ctx, params)
		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: "openai-responses",
			Provider: "openai", Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			if err := processResponsesStream(ctx, stream, out, s); err != nil && out.StopReason != StopError {
				finishError(ctx, out, s, err.Error())
				return
			}
			if out.StopReason == StopPending {
				if len(out.ToolCalls()) > 0 {
					out.StopReason = StopToolUse
				} else {
					out.StopReason = StopStop
				}
			}
			if out.StopReason == StopError || out.StopReason == StopAborted {
				finishError(ctx, out, s, out.ErrorMessage)
				return
			}
			s.push(ctx, Event{Type: EventDone, Reason: out.StopReason, Message: out})
		}()
		return s, nil
	}
}

func buildResponsesInput(reqCtx Context) responses.ResponseInputParam {
	var items responses.ResponseInputParam
	for _, m := range reqCtx.Messages {
		if m.Assistant != nil {
			for _, c := range m.Assistant.Content {
				switch c.Type {
				case KindText:
					if c.Text != "" {
						items = append(items, responses.ResponseInputItemParamOfMessage(c.Text, responses.EasyInputMessageRoleAssistant))
					}
				case KindToolCall:
					args, _ := json.Marshal(c.Arguments)
					items = append(items, responses.ResponseInputItemParamOfFunctionCall(string(args), c.ToolID, c.ToolName))
				}
			}
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			items = append(items, responses.ResponseInputItemParamOfFunctionCallOutput(m.ToolCallID, m.Content))
			continue
		}
		role := responses.EasyInputMessageRoleUser
		switch m.Role {
		case RoleAssistant:
			role = responses.EasyInputMessageRoleAssistant
		case "system":
			role = responses.EasyInputMessageRoleSystem
		}
		items = append(items, responses.ResponseInputItemParamOfMessage(m.Content, role))
	}
	return items
}

func processResponsesStream(ctx context.Context, stream *ssestream.Stream[responses.ResponseStreamEventUnion], out *AssistantMessage, s *EventStream) error {
	textIdx := map[string]int{}
	toolIdx := map[string]int{}
	for stream.Next() {
		ev := stream.Current()
		switch v := ev.AsAny().(type) {
		case responses.ResponseTextDeltaEvent:
			idx, ok := textIdx[v.ItemID]
			if !ok {
				out.Content = append(out.Content, &Content{Type: KindText})
				idx = len(out.Content) - 1
				textIdx[v.ItemID] = idx
				if !s.push(ctx, Event{Type: EventTextStart, ContentIndex: idx, Partial: out}) {
					return ctx.Err()
				}
			}
			out.Content[idx].Text += v.Delta
			if !s.push(ctx, Event{Type: EventTextDelta, ContentIndex: idx, Delta: v.Delta, Partial: out}) {
				return ctx.Err()
			}
		case responses.ResponseOutputItemAddedEvent:
			if v.Item.Type == "function_call" {
				out.Content = append(out.Content, &Content{Type: KindToolCall, ToolID: firstNonEmpty(v.Item.CallID, v.Item.ID), ToolName: v.Item.Name})
				idx := len(out.Content) - 1
				toolIdx[v.Item.ID] = idx
				if v.Item.CallID != "" {
					toolIdx[v.Item.CallID] = idx
				}
				if !s.push(ctx, Event{Type: EventToolCallStart, ContentIndex: idx, Partial: out}) {
					return ctx.Err()
				}
			}
			if v.Item.Type == "reasoning" {
				out.Content = append(out.Content, &Content{Type: KindThinking})
				idx := len(out.Content) - 1
				textIdx["reasoning:"+v.Item.ID] = idx
				if !s.push(ctx, Event{Type: EventThinkingStart, ContentIndex: idx, Partial: out}) {
					return ctx.Err()
				}
			}
		case responses.ResponseFunctionCallArgumentsDeltaEvent:
			idx, ok := toolIdx[v.ItemID]
			if !ok {
				continue
			}
			block := out.Content[idx]
			block.partialJSON += v.Delta
			block.Arguments = parseStreamingJSON(block.partialJSON)
			if !s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: idx, Delta: v.Delta, Partial: out}) {
				return ctx.Err()
			}
		case responses.ResponseReasoningSummaryTextDeltaEvent:
			key := "reasoning:" + v.ItemID
			idx, ok := textIdx[key]
			if !ok {
				continue
			}
			out.Content[idx].Thinking += v.Delta
			if !s.push(ctx, Event{Type: EventThinkingDelta, ContentIndex: idx, Delta: v.Delta, Partial: out}) {
				return ctx.Err()
			}
		case responses.ResponseCompletedEvent:
			if v.Response.Usage.InputTokens != 0 || v.Response.Usage.OutputTokens != 0 {
				out.Usage.Input = int(v.Response.Usage.InputTokens)
				out.Usage.Output = int(v.Response.Usage.OutputTokens)
				out.Usage.TotalTokens = int(v.Response.Usage.TotalTokens)
			}
			out.ResponseID = v.Response.ID
			switch v.Response.Status {
			case "incomplete":
				out.StopReason = StopLength
			case "failed":
				out.StopReason = StopError
				out.ErrorMessage = "openai responses failed"
			}
		case responses.ResponseErrorEvent:
			out.StopReason = StopError
			out.ErrorMessage = v.Message
			return fmt.Errorf("%s", v.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return err
	}
	for i, c := range out.Content {
		switch c.Type {
		case KindText:
			if !s.push(ctx, Event{Type: EventTextEnd, ContentIndex: i, Content: c.Text, Partial: out}) {
				return ctx.Err()
			}
		case KindThinking:
			if !s.push(ctx, Event{Type: EventThinkingEnd, ContentIndex: i, Content: c.Thinking, Partial: out}) {
				return ctx.Err()
			}
		case KindToolCall:
			if c.partialJSON != "" {
				c.Arguments = parseStreamingJSON(c.partialJSON)
				c.partialJSON = ""
			}
			if !s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: i, ToolCall: c, Partial: out}) {
				return ctx.Err()
			}
		}
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
