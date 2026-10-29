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
		base := responsesBaseURL(c.BaseURL)
		optsList := []option.RequestOption{
			option.WithAPIKey(c.APIKey),
			option.WithBaseURL(base + "/"),
		}
		optsList = append(optsList, option.WithHTTPClient(frameSSEClient(c.HTTPClient, opts.OnProviderStreamEvent)))
		sessionValue, setSession, dropSession := responsesOpenCodeHeader(c.Headers, opts)
		for k, v := range c.Headers {
			if dropSession && strings.EqualFold(k, openCodeSessionHeaderName) {
				continue
			}
			optsList = append(optsList, option.WithHeader(k, v))
		}
		for k, v := range sessionAffinityHeaders(opts, affinityResponses) {
			optsList = append(optsList, option.WithHeader(k, v))
		}
		if setSession {
			optsList = append(optsList, option.WithHeader(openCodeSessionHeaderName, sessionValue))
		}
		client := openai.NewClient(optsList...)

		apiID := c.API
		if apiID == "" {
			apiID = "openai-responses"
		}
		provider := recordedProvider(opts, "openai")
		updateEffort := responsesConfigurationEffort(opts, c.APIKey, c.BaseURL)
		responsesAnchor, listedTools := responsesDeferred(reqCtx, opts)
		params := responses.ResponseNewParams{
			Model: shared.ResponsesModel(azureRequestModel(opts.Provider, opts.Model)),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: buildResponsesInput(reqCtx, replayTarget{
					Provider: provider,
					API:      apiID,
					Model:    opts.Model,
				}, responsesAnchor, updateEffort),
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
		if updateEffort != "" {
			// configuration_update items carry the real level. A fixed top-level
			// effort keeps the cached prefix when the level changes.
			params.Reasoning = shared.ReasoningParam{
				Effort:  shared.ReasoningEffort(configurationUpdateRequestEffort),
				Summary: shared.ReasoningSummaryAuto,
			}
			params.Include = []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent}
		} else if effort := reasoningEffort(opts); effort != "" {
			params.Reasoning = shared.ReasoningParam{
				Effort:  shared.ReasoningEffort(effort),
				Summary: shared.ReasoningSummaryAuto,
			}
			params.Include = []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent}
		}
		if len(listedTools) > 0 {
			tools := make([]responses.ToolUnionParam, 0, len(listedTools))
			for _, t := range listedTools {
				ft := responsesFunctionTool(t, false)
				tools = append(tools, responses.ToolUnionParam{OfFunction: &ft})
			}
			params.Tools = tools
		}
		applyResponsesSampling(&params, extra, reqCtx, opts, updateEffort != "")

		stream := client.Responses.NewStreaming(ctx, params)
		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: apiID,
			Provider: provider, Model: opts.Model, StopReason: StopPending,
			ProviderThinkingLevel: updateEffort,
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

// configurationUpdateRequestEffort is the frozen reasoning.effort while
// configuration_update items carry the thinking level. pi v1.3.0 uses medium.
const configurationUpdateRequestEffort = "medium"

func buildResponsesInput(reqCtx Context, target replayTarget, anchor deferredAnchor, configurationEffort string) responses.ResponseInputParam {
	var items responses.ResponseInputParam
	loaded := map[string]struct{}{}
	for _, m := range transformMessages(reqCtx.Messages, target, normalizeResponsesToolCallID) {
		if m.Assistant != nil {
			var output responses.ResponseInputParam
			for _, c := range m.Assistant.Content {
				switch c.Type {
				case KindText:
					if strings.TrimSpace(c.Text) != "" {
						output = append(output, responses.ResponseInputItemParamOfMessage(c.Text, responses.EasyInputMessageRoleAssistant))
					}
				case KindThinking:
					if c.ThinkingSignature != "" {
						item := responses.ResponseInputItemParamOfReasoning("rsn_"+c.ThinkingSignature[:min(len(c.ThinkingSignature), 8)], nil)
						item.OfReasoning.EncryptedContent = param.NewOpt(c.ThinkingSignature)
						output = append(output, item)
					}
				case KindToolCall:
					output = append(output, responsesFunctionCallItem(c))
				}
			}
			if len(output) == 0 {
				continue
			}
			if configurationEffort != "" && sameReplayModel(m.Assistant, target) && m.Assistant.ProviderThinkingLevel != "" {
				items = append(items, responsesConfigurationUpdate(m.Assistant.ProviderThinkingLevel))
			}
			items = append(items, output...)
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
			items = append(items, responsesDeferredItems(m, anchor, loaded)...)
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
	if configurationEffort != "" {
		items = append(items, responsesConfigurationUpdate(configurationEffort))
	}
	return items
}

func responsesConfigurationUpdate(effort string) responses.ResponseInputItemUnionParam {
	return param.Override[responses.ResponseInputItemUnionParam](map[string]any{
		"type":      "configuration_update",
		"reasoning": map[string]any{"effort": effort},
	})
}

func sameReplayModel(msg *AssistantMessage, target replayTarget) bool {
	return msg != nil && msg.Provider == target.Provider && msg.API == target.API && msg.Model == target.Model
}

// responsesBaseURL is the Responses API root, with /v1 appended when missing.
func responsesBaseURL(raw string) string {
	base := strings.TrimRight(raw, "/")
	if base == "" {
		return "https://api.openai.com/v1"
	}
	if !strings.HasSuffix(base, "/v1") && !strings.Contains(base, "/v1/") {
		base += "/v1"
	}
	return base
}

// responsesChatGPTSignIn matches pi v1.3.0: an openai request to the official
// API whose credential is not an sk- key. Sign in with ChatGPT (#389) rejects
// configuration_update items, so that path keeps reasoning.effort.
func responsesChatGPTSignIn(provider, baseURL, apiKey string) bool {
	if provider != "openai" || responsesBaseURL(baseURL) != "https://api.openai.com/v1" {
		return false
	}
	return apiKey != "" && !strings.HasPrefix(apiKey, "sk-")
}

// responsesConfigurationEffort is the effort written into configuration_update
// items. Empty means the level stays on reasoning.effort. A null thinking-level
// map entry (minimal) falls back to the canonical name, matching pi's ??.
func responsesConfigurationEffort(opts Options, apiKey, baseURL string) string {
	if !midConvoEffort(opts) || responsesChatGPTSignIn(opts.Provider, baseURL, apiKey) {
		return ""
	}
	level := strings.ToLower(strings.TrimSpace(opts.Thinking))
	if level == "" || level == "off" {
		return ""
	}
	if m, ok := models.Lookup(opts.Provider, opts.Model); ok {
		if mapped, present := m.ThinkingLevelMap[level]; present {
			// A null map entry is unsupported on the ordinary effort field.
			// configuration_update still sends the canonical name (pi's ??).
			if mapped == nil || strings.TrimSpace(*mapped) == "" {
				return level
			}
			return strings.TrimSpace(*mapped)
		}
	}
	canonical, wire := resolvedThinking(opts)
	if canonical == "" || canonical == "off" {
		return ""
	}
	if wire != "" {
		return wire
	}
	return canonical
}

func responsesFunctionCallItem(c *Content) responses.ResponseInputItemUnionParam {
	args, _ := json.Marshal(c.Arguments)
	item := responses.ResponseInputItemParamOfFunctionCall(string(args), c.ToolID, c.ToolName)
	if c.Namespace != "" && item.OfFunctionCall != nil {
		item.OfFunctionCall.SetExtraFields(map[string]any{"namespace": c.Namespace})
	}
	return item
}

func responsesFunctionTool(t Tool, deferLoading bool) responses.FunctionToolParam {
	parameters := t.Parameters
	strict := toolStrict(t)
	if strict {
		parameters = strictToolParameters(t.Parameters)
	}
	ft := responses.FunctionToolParam{
		Name:       t.Name,
		Parameters: parameters,
		Strict:     param.NewOpt(strict),
	}
	if t.Description != "" {
		ft.Description = param.NewOpt(t.Description)
	}
	if deferLoading {
		ft.SetExtraFields(map[string]any{"defer_loading": true})
	}
	return ft
}

func responsesDeferredItems(m Message, anchor deferredAnchor, loaded map[string]struct{}) []responses.ResponseInputItemUnionParam {
	tools := takeDeferredTools(m.AddedToolNames, anchor.deferred, loaded)
	if len(tools) == 0 {
		return nil
	}
	switch anchor.mode {
	case toolAnchorAdditional:
		defs := make([]responses.FunctionToolParam, 0, len(tools))
		for _, t := range tools {
			defs = append(defs, responsesFunctionTool(t, false))
		}
		return []responses.ResponseInputItemUnionParam{
			param.Override[responses.ResponseInputItemUnionParam](map[string]any{
				"type":  "additional_tools",
				"role":  "developer",
				"tools": defs,
			}),
		}
	case toolAnchorSearch:
		names := make([]string, len(tools))
		defs := make([]responses.FunctionToolParam, 0, len(tools))
		for i, t := range tools {
			names[i] = t.Name
			defs = append(defs, responsesFunctionTool(t, true))
		}
		callID := "pigo_tool_load_" + shortHash8(m.ToolCallID+":"+strings.Join(names, ","))
		return []responses.ResponseInputItemUnionParam{
			param.Override[responses.ResponseInputItemUnionParam](map[string]any{
				"type":      "tool_search_call",
				"call_id":   callID,
				"execution": "client",
				"status":    "completed",
				"arguments": map[string]any{"query": strings.Join(names, " "), "limit": len(names)},
			}),
			param.Override[responses.ResponseInputItemUnionParam](map[string]any{
				"type":      "tool_search_output",
				"call_id":   callID,
				"execution": "client",
				"status":    "completed",
				"tools":     defs,
			}),
		}
	default:
		return nil
	}
}

func functionCallNamespace(item responses.ResponseOutputItemUnion) string {
	if item.Type != "function_call" {
		return ""
	}
	fc := item.AsFunctionCall()
	if field, ok := fc.JSON.ExtraFields["namespace"]; ok && field.Valid() {
		var ns string
		if json.Unmarshal([]byte(field.Raw()), &ns) == nil && ns != "" {
			return ns
		}
	}
	var raw map[string]any
	if json.Unmarshal([]byte(item.RawJSON()), &raw) == nil {
		if ns, ok := raw["namespace"].(string); ok {
			return ns
		}
	}
	return ""
}

func applyFunctionCallNamespace(block *Content, item responses.ResponseOutputItemUnion) {
	if block == nil {
		return
	}
	if ns := functionCallNamespace(item); ns != "" {
		block.Namespace = ns
	}
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
				block := &Content{Type: KindToolCall, ToolID: firstNonEmpty(v.Item.CallID, v.Item.ID), ToolName: v.Item.Name}
				applyFunctionCallNamespace(block, v.Item)
				out.Content = append(out.Content, block)
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
				applyFunctionCallNamespace(block, v.Item)
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
		read := int(resp.Usage.InputTokensDetails.CachedTokens)
		write := responseCacheWriteTokens(resp.Usage.InputTokensDetails)
		applyInclusivePromptUsage(&out.Usage, int(resp.Usage.InputTokens), int(resp.Usage.OutputTokens), read, write)
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

func responseCacheWriteTokens(details responses.ResponseUsageInputTokensDetails) int {
	f, ok := details.JSON.ExtraFields["cache_write_tokens"]
	// The SDK marks this unknown field invalid, but still keeps the raw JSON.
	if !ok || f.Raw() == "" {
		return 0
	}
	var n int
	if err := json.Unmarshal([]byte(f.Raw()), &n); err != nil || n < 0 {
		return 0
	}
	return n
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
