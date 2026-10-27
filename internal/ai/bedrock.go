package ai

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
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

type userAgentRoundTripper struct {
	base http.RoundTripper
	ua   string
	set  bool
}

func (u userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if u.set {
		r.Header.Set("User-Agent", u.ua)
	} else {
		r.Header.Del("User-Agent")
	}
	base := u.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

func withBedrockHTTP(client *http.Client, apiKey string, configured, extra map[string]string) *http.Client {
	ua, set := selectUserAgent(configured, extra)
	if client == nil {
		client = &http.Client{}
	}
	out := *client
	out.Transport = userAgentRoundTripper{base: client.Transport, ua: ua, set: set}
	if apiKey != "" {
		return withBearerHTTP(&out, apiKey)
	}
	return &out
}

// StreamFn returns a StreamFn bound to this client.
func (c *BedrockClient) StreamFn() StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		loadOpts := []func(*awsconfig.LoadOptions) error{
			awsconfig.WithHTTPClient(withBedrockHTTP(c.HTTPClient, c.APIKey, c.Headers, opts.ExtraHeaders)),
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
		if err != nil {
			return errorStreamProvider(opts.Model, "bedrock-converse-stream", err.Error()), nil
		}
		client := bedrockruntime.NewFromConfig(awsCfg)
		name := bedrockCatalogName(opts)
		wire := bedrockWire{ModelName: name, CacheRetention: opts.CacheRetention}
		input := &bedrockruntime.ConverseStreamInput{
			ModelId: aws.String(opts.Model),
			Messages: bedrockMessages(reqCtx, replayTarget{
				Provider: recordedProvider(opts, "amazon-bedrock"),
				API:      "bedrock-converse-stream",
				Model:    opts.Model,
			}, wire),
			System: bedrockSystemBlocks(reqCtx.System, opts.Model, name, opts.CacheRetention),
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
		input.InferenceConfig = bedrockInferenceConfig(recordedProvider(opts, "amazon-bedrock"), opts.Model, name, opts.MaxTokens)
		if fields := bedrockThinkingFields(opts); fields != nil {
			input.AdditionalModelRequestFields = document.NewLazyDocument(fields)
		}

		s := NewEventStream(16)
		out := &AssistantMessage{
			Role: RoleAssistant, Content: []*Content{}, API: "bedrock-converse-stream",
			Provider: recordedProvider(opts, "amazon-bedrock"), Model: opts.Model, StopReason: StopPending,
		}
		go func() {
			defer s.end()
			if !s.push(ctx, Event{Type: EventStart, Partial: out}) {
				return
			}
			resp, err := client.ConverseStream(ctx, input)
			if err != nil {
				finishBedrockError(ctx, out, s, err)
				return
			}
			processBedrockStream(ctx, resp.GetStream(), out, s, opts.OnProviderStreamEvent, catalogCost(opts))
		}()
		return s, nil
	}
}

func bedrockThinkingFields(opts Options) map[string]any {
	name := bedrockCatalogName(opts)
	if !bedrockIsClaude(opts.Model, name) {
		if fields, ok := bedrockOpenAIReasoningFields(opts, name); ok {
			return fields
		}
	}
	canonical, _ := resolvedThinking(opts)
	if !thinkingOn(canonical) && opts.ThinkingBudget <= 0 {
		return nil
	}
	budget := opts.ThinkingBudget
	if budget <= 0 {
		budget = models.BudgetTokens(canonical)
	}
	if budget > 0 {
		return map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": budget}}
	}
	return map[string]any{"thinking": map[string]any{"type": "adaptive"}}
}

// bedrockGPTEffort is the nested reasoning.effort table for GPT models on
// Bedrock. A null thinkingLevelMap entry falls back here instead of clamping.
var bedrockGPTEffort = map[string]string{
	"minimal": "low",
	"low":     "low",
	"medium":  "medium",
	"high":    "high",
	"xhigh":   "xhigh",
	"max":     "max",
}

// bedrockGPTOSSEffort is the flat reasoning_effort table. gpt-oss only
// accepts low, medium, and high, and ignores thinkingLevelMap.
var bedrockGPTOSSEffort = map[string]string{
	"minimal": "low",
	"low":     "low",
	"medium":  "medium",
	"high":    "high",
	"xhigh":   "high",
	"max":     "high",
}

// bedrockOpenAIReasoningFields reports whether model id or name is an OpenAI
// model on Bedrock. The bool is true even when no fields are sent, so a GPT
// model does not fall through to Claude thinking budgets.
func bedrockOpenAIReasoningFields(opts Options, modelName string) (map[string]any, bool) {
	kind := bedrockOpenAIKind(opts.Model, modelName)
	if kind == "" {
		return nil, false
	}
	m, ok := models.Lookup(recordedProvider(opts, "amazon-bedrock"), opts.Model)
	if ok && !m.SupportsReasoning() {
		return nil, true
	}
	level := strings.ToLower(strings.TrimSpace(opts.Thinking))
	if level == "" || level == "off" {
		return nil, true
	}
	if kind == "oss" {
		effort := bedrockGPTOSSEffort[level]
		if effort == "" {
			return nil, true
		}
		return map[string]any{"reasoning_effort": effort}, true
	}
	effort := ""
	if ok {
		if mapped, present := m.ThinkingLevelMap[level]; present && mapped != nil {
			effort = strings.TrimSpace(*mapped)
		}
	}
	if effort == "" {
		effort = bedrockGPTEffort[level]
	}
	if effort == "" {
		return nil, true
	}
	return map[string]any{"reasoning": map[string]any{"effort": effort}}, true
}

func bedrockOpenAIKind(modelID, modelName string) string {
	cands := bedrockMatchCandidates(modelID, modelName)
	for _, s := range cands {
		if strings.Contains(s, "gpt-oss") {
			return "oss"
		}
	}
	for _, s := range cands {
		if strings.Contains(s, "gpt-") {
			return "gpt"
		}
	}
	return ""
}

const bedrockEmptyText = "<empty>"

// bedrockWire carries the catalog name and cache setting used to build a
// Converse request. Model id stays on replayTarget.
type bedrockWire struct {
	ModelName      string
	CacheRetention string
}

func bedrockCatalogName(opts Options) string {
	m, ok := models.Lookup(recordedProvider(opts, "amazon-bedrock"), opts.Model)
	if !ok {
		return ""
	}
	return m.Name
}

func bedrockMessages(reqCtx Context, target replayTarget, wire bedrockWire) []types.Message {
	var out []types.Message
	msgs := transformMessages(reqCtx.Messages, target, normalizeSanitizedToolCallID)
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if m.Assistant != nil {
			var blocks []types.ContentBlock
			for _, c := range m.Assistant.Content {
				switch c.Type {
				case KindText:
					if block := bedrockTextBlock(c.Text, false); block != nil {
						blocks = append(blocks, block)
					}
				case KindThinking:
					if block := bedrockReasoningBlock(c, bedrockIsClaude(target.Model, wire.ModelName)); block != nil {
						blocks = append(blocks, block)
					}
				case KindToolCall:
					blocks = append(blocks, &types.ContentBlockMemberToolUse{Value: types.ToolUseBlock{
						ToolUseId: aws.String(c.ToolID),
						Name:      aws.String(c.ToolName),
						Input:     document.NewLazyDocument(c.Arguments),
					}})
				}
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, types.Message{Role: types.ConversationRoleAssistant, Content: blocks})
			continue
		}
		if bedrockIsToolResult(m) {
			var blocks []types.ContentBlock
			for i < len(msgs) && bedrockIsToolResult(msgs[i]) {
				blocks = append(blocks, bedrockToolResultBlock(msgs[i]))
				i++
			}
			i--
			out = append(out, types.Message{Role: types.ConversationRoleUser, Content: blocks})
			continue
		}
		role := types.ConversationRoleUser
		if m.Role == RoleAssistant {
			role = types.ConversationRoleAssistant
		}
		blocks := []types.ContentBlock{}
		if block := bedrockTextBlock(m.Content, false); block != nil {
			blocks = append(blocks, block)
		}
		for _, img := range m.Images {
			if b := bedrockImageBlock(img); b != nil {
				blocks = append(blocks, b)
			}
		}
		if len(blocks) == 0 {
			blocks = []types.ContentBlock{bedrockTextBlock(bedrockEmptyText, true)}
		}
		out = append(out, types.Message{Role: role, Content: blocks})
	}
	if cp := bedrockCachePoint(target.Model, wire.ModelName, wire.CacheRetention); cp != nil && len(out) > 0 {
		last := &out[len(out)-1]
		if last.Role == types.ConversationRoleUser {
			last.Content = append(last.Content, &types.ContentBlockMemberCachePoint{Value: *cp})
		}
	}
	return out
}

func bedrockSystemBlocks(system, modelID, modelName, retention string) []types.SystemContentBlock {
	if system == "" {
		return nil
	}
	blocks := []types.SystemContentBlock{&types.SystemContentBlockMemberText{Value: system}}
	if cp := bedrockCachePoint(modelID, modelName, retention); cp != nil {
		blocks = append(blocks, &types.SystemContentBlockMemberCachePoint{Value: *cp})
	}
	return blocks
}

func bedrockCachePoint(modelID, modelName, retention string) *types.CachePointBlock {
	if bedrockCacheOff(retention) || !bedrockSupportsPromptCaching(modelID, modelName) {
		return nil
	}
	cp := &types.CachePointBlock{Type: types.CachePointTypeDefault}
	if strings.EqualFold(strings.TrimSpace(retention), "long") {
		cp.Ttl = types.CacheTTLOneHour
	}
	return cp
}

func bedrockCacheOff(retention string) bool {
	return strings.EqualFold(strings.TrimSpace(retention), "none")
}

func bedrockSupportsPromptCaching(modelID, modelName string) bool {
	cands := bedrockMatchCandidates(modelID, modelName)
	hasClaude := false
	for _, s := range cands {
		if strings.Contains(s, "claude") {
			hasClaude = true
			break
		}
	}
	if !hasClaude {
		return os.Getenv("AWS_BEDROCK_FORCE_CACHE") == "1"
	}
	for _, s := range cands {
		if strings.Contains(s, "fable-5") || strings.Contains(s, "opus-5") || strings.Contains(s, "sonnet-5") || strings.Contains(s, "haiku-5") {
			return true
		}
		if strings.Contains(s, "-4-") || strings.Contains(s, "claude-3-7-sonnet") || strings.Contains(s, "claude-3-5-haiku") {
			return true
		}
	}
	return false
}

func bedrockMatchCandidates(modelID, modelName string) []string {
	var out []string
	for _, v := range []string{modelID, modelName} {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		lower := strings.ToLower(v)
		out = append(out, lower, bedrockHyphenate(lower))
	}
	return out
}

func bedrockHyphenate(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		if r == ' ' || r == '_' || r == '.' || r == ':' || r == '\t' || r == '\n' {
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = true
			continue
		}
		dash = false
		b.WriteRune(r)
	}
	return b.String()
}

func bedrockIsClaude(modelID, modelName string) bool {
	id := strings.ToLower(modelID)
	name := strings.ToLower(modelName)
	if strings.Contains(id, "anthropic.claude") || strings.Contains(id, "anthropic/claude") {
		return true
	}
	return strings.Contains(name, "anthropic.claude") || strings.Contains(name, "anthropic/claude") || strings.Contains(name, "claude")
}

func bedrockIsToolResult(m Message) bool {
	return m.Assistant == nil && (m.Role == RoleToolResult || m.ToolCallID != "")
}

func bedrockTextBlock(text string, required bool) types.ContentBlock {
	if strings.TrimSpace(text) == "" {
		if !required {
			return nil
		}
		text = bedrockEmptyText
	}
	return &types.ContentBlockMemberText{Value: text}
}

func bedrockToolResultBlock(m Message) types.ContentBlock {
	var content []types.ToolResultContentBlock
	if strings.TrimSpace(m.Content) != "" {
		content = append(content, &types.ToolResultContentBlockMemberText{Value: m.Content})
	}
	for _, img := range m.Images {
		if b := bedrockImageBlock(img); b != nil {
			content = append(content, &types.ToolResultContentBlockMemberImage{Value: b.Value})
		}
	}
	if len(content) == 0 {
		content = append(content, &types.ToolResultContentBlockMemberText{Value: bedrockEmptyText})
	}
	status := types.ToolResultStatusSuccess
	if m.IsError {
		status = types.ToolResultStatusError
	}
	return &types.ContentBlockMemberToolResult{Value: types.ToolResultBlock{
		ToolUseId: aws.String(m.ToolCallID),
		Content:   content,
		Status:    status,
	}}
}

var bedrockErrorPrefixes = map[string]string{
	"InternalServerException":     "Internal server error",
	"ModelStreamErrorException":   "Model stream error",
	"ValidationException":         "Validation error",
	"ThrottlingException":         "Throttling error",
	"ServiceUnavailableException": "Service unavailable",
}

func finishBedrockError(ctx context.Context, out *AssistantMessage, s *EventStream, err error) {
	if d := bedrockFailureDiagnostic(err); d != nil {
		out.Diagnostics = append(out.Diagnostics, *d)
	}
	finishError(ctx, out, s, formatBedrockError(err))
}

func bedrockFailureDiagnostic(err error) *AssistantDiagnostic {
	if err == nil {
		return nil
	}
	details := DiagnosticDetails{}
	if status, ok := bedrockHTTPStatus(err); ok {
		details.Status = status
	}
	if code := bedrockErrorCode(err); code != "" {
		details.ErrorCode = code
	}
	if id := bedrockRequestID(err); id != "" {
		details.RequestID = id
	}
	if details.Status == 0 && details.ErrorCode == "" && details.RequestID == "" {
		return nil
	}
	return &AssistantDiagnostic{
		Type:      "bedrock_response_failure",
		Timestamp: time.Now().UnixMilli(),
		Details:   &details,
	}
}

func bedrockHTTPStatus(err error) (int, bool) {
	var wrapped *awshttp.ResponseError
	if errors.As(err, &wrapped) {
		if wrapped == nil || wrapped.ResponseError == nil || wrapped.Response == nil || wrapped.Response.Response == nil {
			return 0, false
		}
		if wrapped.Response.StatusCode == 0 {
			return 0, false
		}
		return wrapped.Response.StatusCode, true
	}
	var coder interface{ HTTPStatusCode() int }
	if !errors.As(err, &coder) || coder == nil || coder.HTTPStatusCode() == 0 {
		return 0, false
	}
	return coder.HTTPStatusCode(), true
}

func bedrockRequestID(err error) string {
	var ider interface{ ServiceRequestID() string }
	if !errors.As(err, &ider) || ider == nil {
		return ""
	}
	id := strings.TrimSpace(ider.ServiceRequestID())
	if id == "" || len(id) > 200 {
		return ""
	}
	return id
}

func formatBedrockError(err error) string {
	if err == nil {
		return ""
	}
	core := bedrockErrorCore(err)
	code := bedrockErrorCode(err)
	if code == "" {
		return core
	}
	prefix, ok := bedrockErrorPrefixes[code]
	if !ok {
		prefix = code
	}
	return prefix + ": " + core
}

func bedrockErrorCode(err error) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) && coded != nil {
		return coded.ErrorCode()
	}
	return ""
}

func bedrockErrorCore(err error) string {
	var withMessage interface{ ErrorMessage() string }
	if errors.As(err, &withMessage) && withMessage != nil {
		if msg := withMessage.ErrorMessage(); msg != "" {
			return msg
		}
	}
	return err.Error()
}

func bedrockInferenceConfig(provider, modelID, modelName string, maxTokens int) *types.InferenceConfiguration {
	if maxTokens <= 0 && bedrockIsClaude(modelID, modelName) {
		maxTokens = models.MaxTokens(provider, modelID)
	}
	if maxTokens <= 0 {
		return nil
	}
	return &types.InferenceConfiguration{MaxTokens: aws.Int32(int32(maxTokens))}
}

func bedrockReasoningBlock(c *Content, claude bool) types.ContentBlock {
	if c.Redacted {
		data, err := base64.StdEncoding.DecodeString(c.ThinkingSignature)
		if err != nil {
			data = []byte(c.ThinkingSignature)
		}
		return &types.ContentBlockMemberReasoningContent{
			Value: &types.ReasoningContentBlockMemberRedactedContent{Value: data},
		}
	}
	if strings.TrimSpace(c.Thinking) == "" {
		return nil
	}
	if claude && strings.TrimSpace(c.ThinkingSignature) == "" {
		return &types.ContentBlockMemberText{Value: c.Thinking}
	}
	text := types.ReasoningTextBlock{Text: aws.String(c.Thinking)}
	if claude {
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

func applyBedrockUsage(dst *Usage, u *types.TokenUsage, cost *models.Cost) {
	if dst == nil || u == nil {
		return
	}
	if u.InputTokens != nil {
		dst.Input = int(*u.InputTokens)
	}
	if u.OutputTokens != nil {
		dst.Output = int(*u.OutputTokens)
	}
	if u.CacheReadInputTokens != nil {
		dst.CacheRead = int(*u.CacheReadInputTokens)
	}
	if u.CacheWriteInputTokens != nil {
		dst.CacheWrite = int(*u.CacheWriteInputTokens)
	}
	long := 0
	for _, detail := range u.CacheDetails {
		if detail.Ttl == types.CacheTTLOneHour && detail.InputTokens != nil {
			long += int(*detail.InputTokens)
		}
	}
	dst.CacheWrite1h = long
	dst.TotalTokens = dst.Input + dst.Output + dst.CacheRead + dst.CacheWrite
	calculateCost(cost, dst)
}

func processBedrockStream(ctx context.Context, stream *bedrockruntime.ConverseStreamEventStream, out *AssistantMessage, s *EventStream, observe func([]byte), cost *models.Cost) {
	defer func() { _ = stream.Close() }()
	textIdx := map[int32]int{}
	thinkIdx := map[int32]int{}
	toolIdx := map[int32]int{}
	for ev := range stream.Events() {
		observeSDKEvent(observe, ev)
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
			applyBedrockUsage(&out.Usage, v.Value.Usage, cost)
		}
	}
	if err := stream.Err(); err != nil && out.StopReason != StopError {
		finishBedrockError(ctx, out, s, err)
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
