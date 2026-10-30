package ai

import (
	"math"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

func TestBedrockMessagesToolPair(t *testing.T) {
	got := bedrockMessages(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindText, Text: "yo"},
			{Type: KindToolCall, ToolID: "1", ToolName: "read", Arguments: map[string]any{"path": "a"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "1", ToolName: "read", Content: "ok"},
	}}, replayTarget{}, bedrockWire{})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Role != types.ConversationRoleUser || got[1].Role != types.ConversationRoleAssistant {
		t.Fatalf("roles = %s %s", got[0].Role, got[1].Role)
	}
	if len(got[1].Content) != 2 {
		t.Fatalf("assistant blocks = %d", len(got[1].Content))
	}
	if _, ok := got[2].Content[0].(*types.ContentBlockMemberToolResult); !ok {
		t.Fatalf("want tool result, got %T", got[2].Content[0])
	}
}

func TestBedrockMessagesMergesParallelToolResults(t *testing.T) {
	got := bedrockMessages(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "a", ToolName: "read", Arguments: map[string]any{"path": "a"}},
			{Type: KindToolCall, ToolID: "b", ToolName: "bash", Arguments: map[string]any{"cmd": "ls"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "a", ToolName: "read", Content: "file"},
		{Role: RoleToolResult, ToolCallID: "b", ToolName: "bash", Content: "boom", IsError: true},
		{Role: RoleUser, Content: "next"},
	}}, replayTarget{}, bedrockWire{})
	if len(got) != 4 {
		t.Fatalf("len = %d, want user + assistant + one tool user + follow-up", len(got))
	}
	if got[2].Role != types.ConversationRoleUser || len(got[2].Content) != 2 {
		t.Fatalf("merged tool message = role %s blocks %d", got[2].Role, len(got[2].Content))
	}
	first := bedrockToolResult(t, got[2].Content[0])
	second := bedrockToolResult(t, got[2].Content[1])
	if aws.ToString(first.ToolUseId) != "a" || first.Status != types.ToolResultStatusSuccess {
		t.Fatalf("first result = %+v", first)
	}
	if aws.ToString(second.ToolUseId) != "b" || second.Status != types.ToolResultStatusError {
		t.Fatalf("second result = %+v", second)
	}
	if got[3].Role != types.ConversationRoleUser || len(got[3].Content) != 1 {
		t.Fatalf("follow-up user = role %s blocks %d", got[3].Role, len(got[3].Content))
	}
}

func TestBedrockMessagesRelocatesClaudeErrorToolImages(t *testing.T) {
	id := "anthropic.claude-sonnet-4-6"
	name := "Claude Sonnet 4.6"
	png := ImageContent{Type: "image", Data: "QQ==", MimeType: "image/png"}
	successJSON := `{"content":[{"type":"text","text":"see"},{"type":"image","data":"QQ==","mimeType":"image/png"}]}`
	errJSON := `{"content":[{"type":"text","text":"before"},{"type":"image","data":"QQ==","mimeType":"image/png"},{"type":"image","data":"QQ==","mimeType":"image/jpeg"},{"type":"text","text":"after"}]}`
	got := bedrockMessages(Context{Messages: []Message{
		{Role: RoleUser, Content: "look"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "a", ToolName: "read"},
			{Type: KindToolCall, ToolID: "b", ToolName: "snap"},
			{Type: KindToolCall, ToolID: "c", ToolName: "bash"},
			{Type: KindToolCall, ToolID: "d", ToolName: "shot"},
		}}},
		{Role: RoleToolResult, ToolCallID: "a", ToolName: "read", Content: successJSON, Images: []ImageContent{png}},
		{Role: RoleToolResult, ToolCallID: "b", ToolName: "snap", Content: errJSON, IsError: true},
		{Role: RoleToolResult, ToolCallID: "c", ToolName: "bash", Content: "nope", IsError: true},
		{Role: RoleToolResult, ToolCallID: "d", ToolName: "shot", Content: "plain", IsError: true, Images: []ImageContent{png, {Type: "image", Data: "QQ==", MimeType: "image/webp"}}},
	}}, replayTarget{Model: id}, bedrockWire{ModelName: name, CacheRetention: "short"})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	tool := got[2]
	if tool.Role != types.ConversationRoleUser {
		t.Fatalf("role = %s", tool.Role)
	}
	// tool results a-d, caption+2 images for b, caption+2 images for d, cache point
	if len(tool.Content) != 4+1+2+1+2+1 {
		t.Fatalf("blocks = %d", len(tool.Content))
	}
	success := bedrockToolResult(t, tool.Content[0])
	if success.Status != types.ToolResultStatusSuccess || len(success.Content) != 2 {
		t.Fatalf("success = %+v", success)
	}
	if bedrockToolText(t, success.Content[0]) != successJSON {
		t.Fatalf("success text = %q", bedrockToolText(t, success.Content[0]))
	}
	if _, ok := success.Content[1].(*types.ToolResultContentBlockMemberImage); !ok {
		t.Fatalf("success image = %T", success.Content[1])
	}
	fromJSON := bedrockToolResult(t, tool.Content[1])
	jsonText := bedrockToolText(t, fromJSON.Content[0])
	if fromJSON.Status != types.ToolResultStatusError || len(fromJSON.Content) != 1 || strings.Count(jsonText, "(image attached after the tool results)") != 1 || !strings.Contains(jsonText, "before") || !strings.Contains(jsonText, "after") {
		t.Fatalf("json error = %+v text %q", fromJSON, jsonText)
	}
	plainErr := bedrockToolResult(t, tool.Content[2])
	if plainErr.Status != types.ToolResultStatusError || len(plainErr.Content) != 1 || bedrockToolText(t, plainErr.Content[0]) != "nope" {
		t.Fatalf("plain error = %+v", plainErr)
	}
	fromField := bedrockToolResult(t, tool.Content[3])
	fieldText := bedrockToolText(t, fromField.Content[0])
	if fromField.Status != types.ToolResultStatusError || len(fromField.Content) != 1 || strings.Count(fieldText, "(image attached after the tool results)") != 1 || !strings.Contains(fieldText, "plain") {
		t.Fatalf("field error = %+v text %q", fromField, fieldText)
	}
	if text := bedrockText(t, tool.Content[4]); text != "Images from the error result of tool call b (snap):" {
		t.Fatalf("json caption = %q", text)
	}
	if mime := bedrockImageMIME(t, tool.Content[5]); mime != types.ImageFormatPng {
		t.Fatalf("json image 0 = %s", mime)
	}
	if mime := bedrockImageMIME(t, tool.Content[6]); mime != types.ImageFormatJpeg {
		t.Fatalf("json image 1 = %s", mime)
	}
	if text := bedrockText(t, tool.Content[7]); text != "Images from the error result of tool call d (shot):" {
		t.Fatalf("field caption = %q", text)
	}
	if mime := bedrockImageMIME(t, tool.Content[8]); mime != types.ImageFormatPng {
		t.Fatalf("field image 0 = %s", mime)
	}
	if mime := bedrockImageMIME(t, tool.Content[9]); mime != types.ImageFormatWebp {
		t.Fatalf("field image 1 = %s", mime)
	}
	if _, ok := tool.Content[10].(*types.ContentBlockMemberCachePoint); !ok {
		t.Fatalf("cache point = %T", tool.Content[10])
	}

	followed := bedrockMessages(Context{Messages: []Message{
		{Role: RoleToolResult, ToolCallID: "b", ToolName: "snap", Content: errJSON, IsError: true},
		{Role: RoleUser, Content: "next"},
	}}, replayTarget{Model: id}, bedrockWire{ModelName: name, CacheRetention: "none"})
	if len(followed) != 2 || len(followed[1].Content) != 1 || bedrockText(t, followed[1].Content[0]) != "next" {
		t.Fatalf("follow-up = %#v", followed)
	}
	if _, ok := followed[0].Content[len(followed[0].Content)-1].(*types.ContentBlockMemberImage); !ok {
		t.Fatalf("relocated image = %T", followed[0].Content[len(followed[0].Content)-1])
	}
}

func TestBedrockMessagesKeepsNonClaudeErrorToolImages(t *testing.T) {
	png := ImageContent{Type: "image", Data: "QQ==", MimeType: "image/png"}
	got := bedrockMessages(Context{Messages: []Message{
		{Role: RoleToolResult, ToolCallID: "b", ToolName: "snap", Content: "boom", IsError: true, Images: []ImageContent{png}},
	}}, replayTarget{Model: "amazon.nova-pro-v1:0"}, bedrockWire{ModelName: "Nova Pro"})
	if len(got) != 1 || len(got[0].Content) != 1 {
		t.Fatalf("messages = %d blocks %d", len(got), len(got[0].Content))
	}
	tr := bedrockToolResult(t, got[0].Content[0])
	if tr.Status != types.ToolResultStatusError || len(tr.Content) != 2 {
		t.Fatalf("error result = %+v", tr)
	}
	if bedrockToolText(t, tr.Content[0]) != "boom" {
		t.Fatalf("text = %q", bedrockToolText(t, tr.Content[0]))
	}
	if _, ok := tr.Content[1].(*types.ToolResultContentBlockMemberImage); !ok {
		t.Fatalf("image = %T", tr.Content[1])
	}
}

func TestBedrockMessagesOmitsBlankText(t *testing.T) {
	png := ImageContent{Type: "image", Data: "QQ==", MimeType: "image/png"}
	got := bedrockMessages(Context{Messages: []Message{
		{Role: RoleUser, Content: "   "},
		{Role: RoleUser, Content: "  ", Images: []ImageContent{png}},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindText, Text: " \n"},
			{Type: KindToolCall, ToolID: "1", ToolName: "read", Arguments: map[string]any{}},
		}}},
		{Role: RoleToolResult, ToolCallID: "1", ToolName: "read", Content: " \t"},
	}}, replayTarget{}, bedrockWire{})
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	if text := bedrockText(t, got[0].Content[0]); text != "<empty>" || len(got[0].Content) != 1 {
		t.Fatalf("blank user = %q blocks %d", text, len(got[0].Content))
	}
	if _, ok := got[1].Content[0].(*types.ContentBlockMemberImage); !ok || len(got[1].Content) != 1 {
		t.Fatalf("blank user with image = %#v", got[1].Content)
	}
	if len(got[2].Content) != 1 {
		t.Fatalf("assistant blocks = %d, blank text should be omitted", len(got[2].Content))
	}
	if _, ok := got[2].Content[0].(*types.ContentBlockMemberToolUse); !ok {
		t.Fatalf("assistant block = %T", got[2].Content[0])
	}
	tr := bedrockToolResult(t, got[3].Content[0])
	if len(tr.Content) != 1 || bedrockToolText(t, tr.Content[0]) != "<empty>" {
		t.Fatalf("blank tool result = %#v", tr.Content)
	}
}

func bedrockText(t *testing.T, block types.ContentBlock) string {
	t.Helper()
	tb, ok := block.(*types.ContentBlockMemberText)
	if !ok {
		t.Fatalf("content %T", block)
	}
	return tb.Value
}

func bedrockToolText(t *testing.T, block types.ToolResultContentBlock) string {
	t.Helper()
	tb, ok := block.(*types.ToolResultContentBlockMemberText)
	if !ok {
		t.Fatalf("tool content %T", block)
	}
	return tb.Value
}

func bedrockImageMIME(t *testing.T, block types.ContentBlock) types.ImageFormat {
	t.Helper()
	img, ok := block.(*types.ContentBlockMemberImage)
	if !ok {
		t.Fatalf("content %T", block)
	}
	return img.Value.Format
}

func bedrockToolResult(t *testing.T, block types.ContentBlock) types.ToolResultBlock {
	t.Helper()
	tr, ok := block.(*types.ContentBlockMemberToolResult)
	if !ok {
		t.Fatalf("content %T", block)
	}
	return tr.Value
}

func TestFormatBedrockErrorThrottling(t *testing.T) {
	err := fakeBedrockError{
		code:      "ThrottlingException",
		message:   "Too many tokens, please wait before trying again.",
		status:    429,
		requestID: "req-123",
	}
	msg := formatBedrockError(err)
	assistant := errorAssistant(msg)
	if IsContextOverflow(assistant, 0) {
		t.Fatalf("throttling classified as overflow: %q", msg)
	}
	if !IsRetryableAssistantError(assistant) {
		t.Fatalf("throttling not retryable: %q", msg)
	}
	if msg != "Throttling error: Too many tokens, please wait before trying again." {
		t.Fatalf("message = %q", msg)
	}

	service := formatBedrockError(fakeBedrockError{code: "ServiceUnavailableException", message: "unavailable"})
	if service != "Service unavailable: unavailable" || !IsRetryableAssistantError(errorAssistant(service)) {
		t.Fatalf("service = %q", service)
	}
	internal := formatBedrockError(fakeBedrockError{code: "InternalServerException", message: "boom"})
	if internal != "Internal server error: boom" || !IsRetryableAssistantError(errorAssistant(internal)) {
		t.Fatalf("internal = %q", internal)
	}
	validation := formatBedrockError(fakeBedrockError{code: "ValidationException", message: "bad request"})
	if validation != "Validation error: bad request" || IsRetryableAssistantError(errorAssistant(validation)) {
		t.Fatalf("validation = %q", validation)
	}
	stream := formatBedrockError(fakeBedrockError{code: "ModelStreamErrorException", message: "stream broke"})
	if stream != "Model stream error: stream broke" {
		t.Fatalf("stream = %q", stream)
	}
	unknown := formatBedrockError(fakeBedrockError{code: "ConflictException", message: "conflict"})
	if unknown != "ConflictException: conflict" {
		t.Fatalf("unknown code = %q", unknown)
	}
	plain := formatBedrockError(errString("dial tcp: connection refused"))
	if plain != "dial tcp: connection refused" {
		t.Fatalf("plain = %q", plain)
	}
}

type fakeBedrockError struct {
	code, message, requestID string
	status                   int
}

func (e fakeBedrockError) Error() string {
	if e.code == "" {
		return e.message
	}
	return e.code + ": " + e.message
}

func (e fakeBedrockError) ErrorCode() string        { return e.code }
func (e fakeBedrockError) ErrorMessage() string     { return e.message }
func (e fakeBedrockError) HTTPStatusCode() int      { return e.status }
func (e fakeBedrockError) ServiceRequestID() string { return e.requestID }

type errString string

func (e errString) Error() string { return string(e) }

func TestBedrockCachePoints(t *testing.T) {
	t.Setenv("AWS_BEDROCK_FORCE_CACHE", "")
	ctx := Context{System: "rules", Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "a", ToolName: "read"},
			{Type: KindToolCall, ToolID: "b", ToolName: "bash"},
		}}},
		{Role: RoleToolResult, ToolCallID: "a", Content: "file"},
		{Role: RoleToolResult, ToolCallID: "b", Content: "ok"},
	}}
	id := "anthropic.claude-sonnet-4-6"
	name := "Claude Sonnet 4.6"

	sys := bedrockSystemBlocks("rules", id, name, "short")
	shortPoint := bedrockSystemCachePoint(t, sys)
	if shortPoint.Type != types.CachePointTypeDefault || shortPoint.Ttl != "" {
		t.Fatalf("short system cache = %+v", shortPoint)
	}
	msgs := bedrockMessages(ctx, replayTarget{Model: id}, bedrockWire{ModelName: name, CacheRetention: "short"})
	last := msgs[len(msgs)-1]
	if last.Role != types.ConversationRoleUser || len(last.Content) != 3 {
		t.Fatalf("last message blocks = %d role %s", len(last.Content), last.Role)
	}
	if _, ok := last.Content[0].(*types.ContentBlockMemberToolResult); !ok {
		t.Fatalf("cache point nested or missing tool result: %T", last.Content[0])
	}
	userPoint := bedrockContentCachePoint(t, last.Content[2])
	if userPoint.Ttl != "" {
		t.Fatalf("short user ttl = %q", userPoint.Ttl)
	}

	longSys := bedrockSystemBlocks("rules", id, name, "long")
	if bedrockSystemCachePoint(t, longSys).Ttl != types.CacheTTLOneHour {
		t.Fatal("long system cache missing 1h ttl")
	}
	longMsgs := bedrockMessages(ctx, replayTarget{Model: id}, bedrockWire{ModelName: name, CacheRetention: "long"})
	longLast := longMsgs[len(longMsgs)-1]
	if bedrockContentCachePoint(t, longLast.Content[len(longLast.Content)-1]).Ttl != types.CacheTTLOneHour {
		t.Fatal("long user cache missing 1h ttl")
	}

	if len(bedrockSystemBlocks("rules", id, name, "none")) != 1 {
		t.Fatal("none should keep system text only")
	}
	noneMsgs := bedrockMessages(ctx, replayTarget{Model: id}, bedrockWire{ModelName: name, CacheRetention: "none"})
	noneLast := noneMsgs[len(noneMsgs)-1]
	if _, ok := noneLast.Content[len(noneLast.Content)-1].(*types.ContentBlockMemberCachePoint); ok {
		t.Fatal("none wrote a user cache point")
	}

	if len(bedrockSystemBlocks("rules", "amazon.nova-lite-v1:0", "Nova Lite", "short")) != 1 {
		t.Fatal("nova should not get a cache point")
	}
	if len(bedrockSystemBlocks("rules", "anthropic.claude-3-opus-20240229-v1:0", "Claude 3 Opus", "short")) != 1 {
		t.Fatal("claude 3 opus should not get a cache point")
	}
	if len(bedrockSystemBlocks("rules", "anthropic.claude-3-5-haiku-20241022-v1:0", "Claude 3.5 Haiku", "short")) != 2 {
		t.Fatal("claude 3.5 haiku should cache")
	}
	if bedrockSystemBlocks("", id, name, "short") != nil {
		t.Fatal("empty system should stay empty")
	}

	arn := "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
	if len(bedrockSystemBlocks("rules", arn, "Claude Sonnet 4.6", "")) != 2 {
		t.Fatal("inference profile name should enable cache points")
	}
	if len(bedrockSystemBlocks("rules", arn, "", "short")) != 1 {
		t.Fatal("arn without a claude name should not cache")
	}
	t.Setenv("AWS_BEDROCK_FORCE_CACHE", "1")
	if len(bedrockSystemBlocks("rules", arn, "", "short")) != 2 {
		t.Fatal("FORCE_CACHE should enable cache points for an unnamed profile")
	}
	if len(bedrockSystemBlocks("rules", "anthropic.claude-3-opus-20240229-v1:0", "Claude 3 Opus", "short")) != 1 {
		t.Fatal("FORCE_CACHE should not override an unsupported claude")
	}
}

func bedrockSystemCachePoint(t *testing.T, blocks []types.SystemContentBlock) types.CachePointBlock {
	t.Helper()
	if len(blocks) != 2 {
		t.Fatalf("system blocks = %d", len(blocks))
	}
	cp, ok := blocks[1].(*types.SystemContentBlockMemberCachePoint)
	if !ok {
		t.Fatalf("system[1] = %T", blocks[1])
	}
	return cp.Value
}

func bedrockContentCachePoint(t *testing.T, block types.ContentBlock) types.CachePointBlock {
	t.Helper()
	cp, ok := block.(*types.ContentBlockMemberCachePoint)
	if !ok {
		t.Fatalf("content %T", block)
	}
	return cp.Value
}

func TestBedrockInferenceConfig(t *testing.T) {
	const id = "anthropic.claude-sonnet-4-6"
	want := int32(models.MaxTokens("amazon-bedrock", id))
	cfg := bedrockInferenceConfig("amazon-bedrock", id, "Claude Sonnet 4.6", 0)
	if cfg == nil || aws.ToInt32(cfg.MaxTokens) != want || want <= 0 {
		t.Fatalf("claude default = %+v, catalog %d", cfg, want)
	}
	explicit := bedrockInferenceConfig("amazon-bedrock", id, "Claude Sonnet 4.6", 50)
	if explicit == nil || aws.ToInt32(explicit.MaxTokens) != 50 {
		t.Fatalf("explicit = %+v", explicit)
	}
	if bedrockInferenceConfig("amazon-bedrock", "amazon.nova-lite-v1:0", "Nova Lite", 0) != nil {
		t.Fatal("nova should omit max tokens when unset")
	}
	nova := bedrockInferenceConfig("amazon-bedrock", "amazon.nova-lite-v1:0", "Nova Lite", 20)
	if nova == nil || aws.ToInt32(nova.MaxTokens) != 20 {
		t.Fatalf("nova explicit = %+v", nova)
	}
	arn := "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
	if bedrockInferenceConfig("amazon-bedrock", arn, "Claude Sonnet 4.6", 0) != nil {
		t.Fatal("unnamed catalog arn has no output cap to send")
	}
}

func TestBedrockReasoningSignature(t *testing.T) {
	claude := replayTarget{Provider: "amazon-bedrock", API: "bedrock-converse-stream", Model: "anthropic.claude-sonnet-4-6"}
	wire := bedrockWire{ModelName: "Claude Sonnet 4.6"}
	signed := bedrockThinkingMessage(t, claude, wire, &Content{Type: KindThinking, Thinking: "plan", ThinkingSignature: "sig"})
	reasoning, ok := signed.(*types.ContentBlockMemberReasoningContent)
	if !ok {
		t.Fatalf("signed claude = %T", signed)
	}
	text, ok := reasoning.Value.(*types.ReasoningContentBlockMemberReasoningText)
	if !ok || aws.ToString(text.Value.Text) != "plan" || aws.ToString(text.Value.Signature) != "sig" {
		t.Fatalf("signed text = %+v", reasoning.Value)
	}

	plain := bedrockThinkingMessage(t, claude, wire, &Content{Type: KindThinking, Thinking: "plan"})
	if tb, ok := plain.(*types.ContentBlockMemberText); !ok || tb.Value != "plan" {
		t.Fatalf("unsigned claude = %T %#v", plain, plain)
	}

	nova := replayTarget{Provider: "amazon-bedrock", API: "bedrock-converse-stream", Model: "amazon.nova-lite-v1:0"}
	novaBlock := bedrockThinkingMessage(t, nova, bedrockWire{ModelName: "Nova Lite"}, &Content{Type: KindThinking, Thinking: "plan", ThinkingSignature: "sig"})
	novaReasoning, ok := novaBlock.(*types.ContentBlockMemberReasoningContent)
	if !ok {
		t.Fatalf("nova = %T", novaBlock)
	}
	novaText, ok := novaReasoning.Value.(*types.ReasoningContentBlockMemberReasoningText)
	if !ok || aws.ToString(novaText.Value.Text) != "plan" || novaText.Value.Signature != nil {
		t.Fatalf("nova text = %+v", novaReasoning.Value)
	}

	dropped := bedrockMessages(Context{Messages: []Message{{Assistant: &AssistantMessage{
		Provider: claude.Provider, API: claude.API, Model: claude.Model,
		Content: []*Content{{Type: KindThinking, Thinking: "  ", ThinkingSignature: "sig"}},
	}}}}, claude, wire)
	if len(dropped) != 0 {
		t.Fatalf("blank thinking messages = %d", len(dropped))
	}
}

func bedrockThinkingMessage(t *testing.T, target replayTarget, wire bedrockWire, block *Content) types.ContentBlock {
	t.Helper()
	got := bedrockMessages(Context{Messages: []Message{{Assistant: &AssistantMessage{
		Provider: target.Provider, API: target.API, Model: target.Model,
		Content: []*Content{block},
	}}}}, target, wire)
	if len(got) != 1 || len(got[0].Content) != 1 {
		t.Fatalf("messages = %d", len(got))
	}
	return got[0].Content[0]
}

func TestBedrockFailureDiagnostic(t *testing.T) {
	err := fakeBedrockError{
		code:      "ThrottlingException",
		message:   "Too many tokens, please wait before trying again.",
		status:    429,
		requestID: "req-123",
	}
	diag := bedrockFailureDiagnostic(err)
	if diag == nil || diag.Type != "bedrock_response_failure" || diag.Timestamp == 0 || diag.Details == nil {
		t.Fatalf("diagnostic = %+v", diag)
	}
	if diag.Details.Status != 429 || diag.Details.ErrorCode != "ThrottlingException" || diag.Details.RequestID != "req-123" {
		t.Fatalf("details = %+v", diag.Details)
	}
	if formatBedrockError(err) != "Throttling error: Too many tokens, please wait before trying again." {
		t.Fatal("diagnostic must leave the retryable error message intact")
	}

	trimmed := bedrockFailureDiagnostic(fakeBedrockError{status: 500, requestID: "  req  "})
	if trimmed == nil || trimmed.Details.Status != 500 || trimmed.Details.RequestID != "req" || trimmed.Details.ErrorCode != "" {
		t.Fatalf("trimmed = %+v", trimmed)
	}
	if bedrockFailureDiagnostic(fakeBedrockError{requestID: strings.Repeat("a", 201)}) != nil {
		t.Fatal("over-long request id should be dropped")
	}
	if bedrockFailureDiagnostic(errString("dial tcp: connection refused")) != nil {
		t.Fatal("plain error should not grow a diagnostic")
	}
}

func TestApplyBedrockUsageCacheTTL(t *testing.T) {
	sonnet := &models.Cost{Input: 3, Output: 15, CacheRead: 0.30, CacheWrite: 3.75}
	both := &types.TokenUsage{
		InputTokens:           aws.Int32(10),
		OutputTokens:          aws.Int32(5),
		CacheReadInputTokens:  aws.Int32(100),
		CacheWriteInputTokens: aws.Int32(1_000_000),
		CacheDetails: []types.CacheDetail{
			{Ttl: types.CacheTTLOneHour, InputTokens: aws.Int32(400_000)},
			{Ttl: types.CacheTTLFiveMinutes, InputTokens: aws.Int32(600_000)},
		},
	}
	var split Usage
	applyBedrockUsage(&split, both, sonnet)
	if split.CacheWrite != 1_000_000 || split.CacheWrite1h != 400_000 || split.CacheRead != 100 {
		t.Fatalf("usage=%+v", split)
	}
	if split.TotalTokens != 10+5+100+1_000_000 {
		t.Fatalf("total=%d, 1h must stay inside cacheWrite", split.TotalTokens)
	}
	if math.Abs(split.Cost.CacheWrite-4.65) > 1e-9 {
		t.Fatalf("cache write cost=%v, want 4.65", split.Cost.CacheWrite)
	}

	var shortOnly Usage
	applyBedrockUsage(&shortOnly, &types.TokenUsage{
		CacheWriteInputTokens: aws.Int32(1_000_000),
	}, sonnet)
	if shortOnly.CacheWrite != 1_000_000 || shortOnly.CacheWrite1h != 0 {
		t.Fatalf("no details should be five-minute writes: %+v", shortOnly)
	}
	if math.Abs(shortOnly.Cost.CacheWrite-3.75) > 1e-9 {
		t.Fatalf("five-minute cost=%v, want 3.75", shortOnly.Cost.CacheWrite)
	}

	var hourOnly Usage
	applyBedrockUsage(&hourOnly, &types.TokenUsage{
		CacheWriteInputTokens: aws.Int32(400_000),
		CacheDetails: []types.CacheDetail{
			{Ttl: types.CacheTTLOneHour, InputTokens: aws.Int32(400_000)},
		},
	}, sonnet)
	if hourOnly.CacheWrite1h != 400_000 || math.Abs(hourOnly.Cost.CacheWrite-2.4) > 1e-9 {
		t.Fatalf("one-hour usage=%+v", hourOnly)
	}
}

func TestBedrockOpenAIReasoningFields(t *testing.T) {
	const provider = "amazon-bedrock"

	assertNested := func(model, level, effort string) {
		t.Helper()
		got := bedrockThinkingFields(Options{Provider: provider, Model: model, Thinking: level})
		reasoning, _ := got["reasoning"].(map[string]any)
		if reasoning["effort"] != effort {
			t.Fatalf("%s %s = %#v", model, level, got)
		}
		if _, ok := got["thinking"]; ok {
			t.Fatalf("%s %s sent thinking: %#v", model, level, got)
		}
		if _, ok := got["reasoning_effort"]; ok {
			t.Fatalf("%s %s sent reasoning_effort: %#v", model, level, got)
		}
	}
	for _, level := range []string{"low", "high", "xhigh"} {
		assertNested("global.openai.gpt-6-sol", level, level)
	}
	assertNested("openai.gpt-5.4", "minimal", "low")
	assertNested("openai.gpt-5.4", "max", "max")
	if got := bedrockThinkingFields(Options{Provider: provider, Model: "openai.gpt-5.4", Thinking: "off"}); got != nil {
		t.Fatalf("off = %#v", got)
	}
	if got := bedrockThinkingFields(Options{Provider: provider, Model: "openai.gpt-5.4", Thinking: ""}); got != nil {
		t.Fatalf("empty = %#v", got)
	}

	assertFlat := func(level, effort string) {
		t.Helper()
		got := bedrockThinkingFields(Options{Provider: provider, Model: "openai.gpt-oss-120b", Thinking: level})
		if got["reasoning_effort"] != effort {
			t.Fatalf("oss %s = %#v", level, got)
		}
		if _, ok := got["reasoning"]; ok || got["thinking"] != nil {
			t.Fatalf("oss %s extra fields: %#v", level, got)
		}
	}
	assertFlat("xhigh", "high")
	assertFlat("max", "high")
	assertFlat("low", "low")

	const arnProvider = "bedrock-arn-effort"
	arn := "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
	noReasoning := false
	models.RegisterProvider(models.ProviderSpec{
		ID: arnProvider, DefaultAPI: "bedrock-converse-stream", DefaultID: arn,
		Models: []models.Model{
			{
				Provider: arnProvider, ID: arn, Name: "GPT-6 Sol",
				Reasoning: boolPtrAI(true),
				ThinkingLevelMap: map[string]*string{
					"high": strPtrAI("xhigh"),
				},
			},
			{
				Provider: arnProvider, ID: "openai.gpt-5-noreason", Name: "GPT-5",
				Reasoning: &noReasoning,
			},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider(arnProvider) })

	got := bedrockThinkingFields(Options{Provider: arnProvider, Model: arn, Thinking: "high"})
	reasoning, _ := got["reasoning"].(map[string]any)
	if reasoning["effort"] != "xhigh" {
		t.Fatalf("arn name match = %#v", got)
	}
	if _, ok := got["thinking"]; ok {
		t.Fatalf("arn sent thinking: %#v", got)
	}
	if got := bedrockThinkingFields(Options{Provider: arnProvider, Model: "openai.gpt-5-noreason", Thinking: "high"}); got != nil {
		t.Fatalf("reasoning false = %#v", got)
	}

	claude := bedrockThinkingFields(Options{Provider: provider, Model: "us.anthropic.claude-opus-4-6-v1", Thinking: "high"})
	thinking, _ := claude["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Fatalf("claude = %#v", claude)
	}
	if _, ok := claude["reasoning"]; ok {
		t.Fatalf("claude sent reasoning: %#v", claude)
	}
	if _, ok := claude["reasoning_effort"]; ok {
		t.Fatalf("claude sent reasoning_effort: %#v", claude)
	}
}
