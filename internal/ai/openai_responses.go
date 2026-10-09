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

	"github.com/Lowpower/pigo/internal/models"
)

// OpenAIResponsesClient talks to the OpenAI Responses API.
type OpenAIResponsesClient struct {
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
	API        string
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
		optsList = append(optsList, option.WithHTTPClient(frameSSEClient(c.HTTPClient, opts.OnProviderStreamEvent)))
		for k, v := range c.Headers {
			optsList = append(optsList, option.WithHeader(k, v))
		}
		for k, v := range sessionAffinityHeaders(opts, affinityResponses) {
			optsList = append(optsList, option.WithHeader(k, v))
		}
		client := openai.NewClient(optsList...)

		apiID := c.API
		if apiID == "" {
			apiID = "openai-responses"
		}
		provider := recordedProvider(opts, "openai")
		params := responses.ResponseNewParams{
			Model: shared.ResponsesModel(opts.Model),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: buildResponsesInput(reqCtx, replayTarget{
					Provider: provider,
					API:      apiID,
					Model:    opts.Model,
				}),
			},
			Store: param.NewOpt(false),
		}
		if reqCtx.System != "" {
			params.Instructions = param.NewOpt(reqCtx.System)
		}
		if opts.MaxTokens > 0 && supportsMaxOutputTokens(opts) {
			n := int64(opts.MaxTokens)
			if n < 16 {
				n = 16
			}
			params.MaxOutputTokens = param.NewOpt(n)
		}
		if key := clampPromptCacheKey(opts.SessionID); key != "" {
			params.PromptCacheKey = param.NewOpt(key)
		}
		extra := map[string]any{}
		if promptCacheRetention24h(opts) {
			extra["prompt_cache_retention"] = "24h"
		}
		if cacheOpts := promptCacheOptions(opts); cacheOpts != nil {
			extra["prompt_cache_options"] = cacheOpts
		}
		if effort := reasoningEffort(opts); effort != "" {
			params.Reasoning = shared.ReasoningParam{
				Effort:  shared.ReasoningEffort(effort),
				Summary: shared.ReasoningSummaryAuto,
			}
			params.Include = []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent}
		}
		if len(reqCtx.Tools) > 0 {
			tools := make([]responses.ToolUnionParam, 0, len(reqCtx.Tools))
			for _, t := range reqCtx.Tools {
				ft := responses.FunctionToolParam{
					Name:       t.Name,
					Parameters: t.Parameters,
					Strict:     param.NewOpt(toolStrict(t)),
				}
				if t.Description != "" {
					ft.Description = param.NewOpt(t.Description)
				}
				tools = append(tools, responses.ToolUnionParam{OfFunction: &ft})
			}
			params.Tools = tools
		}
		applyResponsesSampling(&params, extra, reqCtx, opts)

		stream := client.Responses.NewStreaming(ctx, params)
		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: apiID,
			Provider: provider, Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			if err := processResponsesStream(ctx, stream, out, s, catalogCost(opts)); err != nil && out.StopReason != StopError {
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

func buildResponsesInput(reqCtx Context, target replayTarget) responses.ResponseInputParam {
	var items responses.ResponseInputParam
	for _, m := range transformMessages(reqCtx.Messages, target, normalizeResponsesToolCallID) {
		if m.Assistant != nil {
			for _, c := range m.Assistant.Content {
				switch c.Type {
				case KindText:
					if strings.TrimSpace(c.Text) != "" {
						items = append(items, responses.ResponseInputItemParamOfMessage(c.Text, responses.EasyInputMessageRoleAssistant))
					}
				case KindThinking:
					if c.ThinkingSignature != "" {
						item := responses.ResponseInputItemParamOfReasoning("rsn_"+c.ThinkingSignature[:min(len(c.ThinkingSignature), 8)], nil)
						item.OfReasoning.EncryptedContent = param.NewOpt(c.ThinkingSignature)
						items = append(items, item)
					}
				case KindToolCall:
					args, _ := json.Marshal(c.Arguments)
					items = append(items, responses.ResponseInputItemParamOfFunctionCall(string(args), c.ToolID, c.ToolName))
				}
			}
			continue
		}
		if m.Role == RoleToolResult || m.ToolCallID != "" {
			text, imgs := ParseToolContent(m.Content)
			if text == "" {
				text = m.Content
			}
			if len(imgs) == 0 {
				imgs = m.Images
			}
			items = append(items, responses.ResponseInputItemParamOfFunctionCallOutput(m.ToolCallID, text))
			if len(imgs) > 0 {
				items = append(items, responsesUserImageMessage("", imgs))
			}
			continue
		}
		role := responses.EasyInputMessageRoleUser
		switch m.Role {
		case RoleAssistant:
			role = responses.EasyInputMessageRoleAssistant
		case "system":
			role = responses.EasyInputMessageRoleSystem
		}
		if len(m.Images) == 0 {
			items = append(items, responses.ResponseInputItemParamOfMessage(m.Content, role))
			continue
		}
		items = append(items, responsesUserImageMessage(m.Content, m.Images))
	}
	return items
}

func responsesUserImageMessage(text string, imgs []ImageContent) responses.ResponseInputItemUnionParam {
	content := responses.ResponseInputMessageContentListParam{}
	if text != "" {
		content = append(content, responses.ResponseInputContentParamOfInputText(text))
	}
	for _, img := range imgs {
		part := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
		part.OfInputImage.ImageURL = param.NewOpt("data:" + img.MimeType + ";base64," + img.Data)
		content = append(content, part)
	}
	return responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
}

// responsesIndex keys a streamed output item. Events that omit output_index
// share one sentinel so they cannot collide with a real index of 0.
type responsesIndex struct {
	missing bool
	index   int64
}

func responsesOutputKey(valid bool, index int64) responsesIndex {
	if !valid {
		return responsesIndex{missing: true}
	}
	return responsesIndex{index: index}
}

func processResponsesStream(ctx context.Context, stream *ssestream.Stream[responses.ResponseStreamEventUnion], out *AssistantMessage, s *EventStream, cost *models.Cost) error {
	textIdx := map[string]int{}
	toolSlots := map[responsesIndex]int{}
	sawTerminal := false
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
				toolSlots[responsesOutputKey(v.JSON.OutputIndex.Valid(), v.OutputIndex)] = idx
				if !s.push(ctx, Event{Type: EventToolCallStart, ContentIndex: idx, Partial: out}) {
					return ctx.Err()
				}
			}
			if v.Item.Type == "reasoning" {
				out.Content = append(out.Content, &Content{Type: KindThinking, ThinkingSignature: v.Item.EncryptedContent})
				idx := len(out.Content) - 1
				textIdx["reasoning:"+v.Item.ID] = idx
				if !s.push(ctx, Event{Type: EventThinkingStart, ContentIndex: idx, Partial: out}) {
					return ctx.Err()
				}
			}
		case responses.ResponseOutputItemDoneEvent:
			if v.Item.Type == "reasoning" && v.Item.EncryptedContent != "" {
				key := "reasoning:" + v.Item.ID
				if idx, ok := textIdx[key]; ok {
					out.Content[idx].ThinkingSignature = v.Item.EncryptedContent
				}
			}
			if v.Item.Type == "function_call" {
				key := responsesOutputKey(v.JSON.OutputIndex.Valid(), v.OutputIndex)
				idx, ok := toolSlots[key]
				if !ok {
					break
				}
				block := out.Content[idx]
				if v.Item.Arguments != "" {
					block.partialJSON = v.Item.Arguments
					block.Arguments = parseStreamingJSON(block.partialJSON)
				}
				block.partialJSON = ""
				block.toolDone = true
				delete(toolSlots, key)
			}
		case responses.ResponseFunctionCallArgumentsDeltaEvent:
			idx, ok := toolSlots[responsesOutputKey(v.JSON.OutputIndex.Valid(), v.OutputIndex)]
			if !ok {
				continue
			}
			block := out.Content[idx]
			block.partialJSON += v.Delta
			block.Arguments = parseStreamingJSON(block.partialJSON)
			if !s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: idx, Delta: v.Delta, Partial: out}) {
				return ctx.Err()
			}
		case responses.ResponseFunctionCallArgumentsDoneEvent:
			idx, ok := toolSlots[responsesOutputKey(v.JSON.OutputIndex.Valid(), v.OutputIndex)]
			if !ok {
				continue
			}
			block := out.Content[idx]
			prev := block.partialJSON
			block.partialJSON = v.Arguments
			block.Arguments = parseStreamingJSON(block.partialJSON)
			if strings.HasPrefix(v.Arguments, prev) {
				if delta := v.Arguments[len(prev):]; delta != "" {
					if !s.push(ctx, Event{Type: EventToolCallDelta, ContentIndex: idx, Delta: delta, Partial: out}) {
						return ctx.Err()
					}
				}
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
			sawTerminal = true
			finalizeResponsesTerminal(out, v.Response, cost)
		case responses.ResponseIncompleteEvent:
			sawTerminal = true
			finalizeResponsesTerminal(out, v.Response, cost)
		case responses.ResponseFailedEvent:
			sawTerminal = true
			finalizeResponsesTerminal(out, v.Response, cost)
		case responses.ResponseErrorEvent:
			out.StopReason = StopError
			out.ErrorMessage = v.Message
			return fmt.Errorf("%s", v.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return err
	}
	if !sawTerminal {
		return fmt.Errorf("OpenAI Responses stream ended before a terminal response event")
	}
	if out.StopReason == StopPending {
		for _, c := range out.Content {
			if c.Type == KindToolCall && !c.toolDone {
				msg := fmt.Sprintf("OpenAI Responses stream completed with an unfinished tool call: %s (%s)", c.ToolName, c.ToolID)
				out.StopReason = StopError
				out.ErrorMessage = msg
				return fmt.Errorf("%s", msg)
			}
		}
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
			if !c.toolDone {
				continue
			}
			if !s.push(ctx, Event{Type: EventToolCallEnd, ContentIndex: i, ToolCall: c, Partial: out}) {
				return ctx.Err()
			}
		}
	}
	return nil
}

func finalizeResponsesTerminal(out *AssistantMessage, resp responses.Response, cost *models.Cost) {
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		out.Usage.Input = int(resp.Usage.InputTokens)
		out.Usage.Output = int(resp.Usage.OutputTokens)
		out.Usage.TotalTokens = int(resp.Usage.TotalTokens)
	}
	calculateCost(cost, &out.Usage)
	applyServiceTierCost(&out.Usage, string(resp.ServiceTier))
	if resp.ID != "" {
		out.ResponseID = resp.ID
	}
	switch string(resp.Status) {
	case "incomplete":
		reason := ""
		if resp.IncompleteDetails.JSON.Reason.Valid() {
			reason = resp.IncompleteDetails.Reason
		}
		if reason == "max_output_tokens" {
			out.StopReason = StopLength
			return
		}
		out.StopReason = StopError
		if reason == "" {
			out.ErrorMessage = "Response incomplete without a provider reason"
			return
		}
		out.ErrorMessage = "Response incomplete: " + reason
	case "failed":
		out.StopReason = StopError
		out.ErrorMessage = responsesFailedMessage(resp)
	}
}

func responsesFailedMessage(resp responses.Response) string {
	code := string(resp.Error.Code)
	msg := resp.Error.Message
	if resp.Error.JSON.Code.Valid() && resp.Error.JSON.Message.Valid() && code != "" && msg != "" {
		return code + ": " + msg
	}
	if resp.Error.JSON.Message.Valid() && msg != "" {
		return msg
	}
	if resp.Error.JSON.Code.Valid() && code != "" {
		return code
	}
	return "openai responses failed"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
