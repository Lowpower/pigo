package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestSanitizeSkipsAbortedAndErrorAssistants(t *testing.T) {
	aborted := &AssistantMessage{
		StopReason: StopAborted,
		Content: []*Content{
			{Type: KindText, Text: "partial"},
			{Type: KindToolCall, ToolID: "toolu_1", ToolName: "bash"},
		},
	}
	errored := &AssistantMessage{
		StopReason:   StopError,
		ErrorMessage: "boom",
		Content:      []*Content{{Type: KindText, Text: "half"}},
	}
	in := []Message{
		{Role: RoleUser, Content: "do it"},
		{Role: RoleAssistant, Assistant: aborted},
		{Role: RoleUser, Content: "try again"},
		{Role: RoleAssistant, Assistant: errored},
		{Role: RoleUser, Content: "and now?"},
	}
	got := sanitizeProviderMessages(in, true)
	if len(got) != 3 {
		t.Fatalf("len = %d, messages = %#v", len(got), got)
	}
	for _, m := range got {
		if m.Assistant != nil {
			t.Fatalf("assistant replayed: %#v", m.Assistant)
		}
	}
	if got[0].Content != "do it" || got[1].Content != "try again" || got[2].Content != "and now?" {
		t.Fatalf("users = %#v", got)
	}
	if aborted.Content[0].Text != "partial" || errored.StopReason != StopError {
		t.Fatal("input assistant changed")
	}
}

func TestSanitizeClosesOrphanToolCalls(t *testing.T) {
	kept := &AssistantMessage{
		StopReason: StopToolUse,
		Content:    []*Content{{Type: KindToolCall, ToolID: "toolu_kept", ToolName: "read"}},
	}
	middle := &AssistantMessage{
		StopReason: StopToolUse,
		Content:    []*Content{{Type: KindToolCall, ToolID: "toolu_mid", ToolName: "bash"}},
	}
	end := &AssistantMessage{
		StopReason: StopToolUse,
		Content:    []*Content{{Type: KindToolCall, ToolID: "toolu_end", ToolName: "bash"}},
	}
	in := []Message{
		{Role: RoleUser, Content: "do it"},
		{Role: RoleAssistant, Assistant: kept},
		{Role: RoleToolResult, ToolCallID: "toolu_kept", ToolName: "read", Content: "file"},
		{Role: RoleUser, Content: "try again"},
		{Role: RoleAssistant, Assistant: middle},
		{Role: RoleUser, Content: "and now?"},
		{Role: RoleAssistant, Assistant: end},
	}
	got := sanitizeProviderMessages(in, true)
	var synthetic []Message
	for _, m := range got {
		if m.Content == "No result provided" {
			if !m.IsError || m.Role != RoleToolResult {
				t.Fatalf("synthetic = %#v", m)
			}
			synthetic = append(synthetic, m)
		}
	}
	if len(synthetic) != 2 || synthetic[0].ToolCallID != "toolu_mid" || synthetic[1].ToolCallID != "toolu_end" {
		t.Fatalf("synthetic = %#v", synthetic)
	}
	if countContent(got, "file") != 1 || countToolResults(got, "toolu_kept") != 1 {
		t.Fatalf("existing result duplicated: %#v", got)
	}
	midAt := indexToolResult(got, "toolu_mid")
	userAt := indexContent(got, "and now?")
	endAt := indexToolResult(got, "toolu_end")
	if midAt < 0 || userAt < 0 || midAt > userAt {
		t.Fatalf("middle close at %d, user at %d", midAt, userAt)
	}
	if endAt != len(got)-1 {
		t.Fatalf("end close at %d, len %d", endAt, len(got))
	}
	if kept.Content[0].ToolID != "toolu_kept" {
		t.Fatal("input tool id changed")
	}
}

func TestSanitizeHoldsSystemUntilToolResultsClose(t *testing.T) {
	asst := &AssistantMessage{
		StopReason: StopToolUse,
		Content: []*Content{
			{Type: KindToolCall, ToolID: "call_a", ToolName: "read"},
			{Type: KindToolCall, ToolID: "call_b", ToolName: "bash"},
		},
	}
	in := []Message{
		{Role: RoleAssistant, Assistant: asst},
		{Role: RoleSystem, Content: "mid-system"},
		{Role: RoleToolResult, ToolCallID: "call_a", ToolName: "read", Content: "ok"},
		{Role: RoleUser, Content: "next"},
	}
	got := sanitizeProviderMessages(in, true)
	if len(got) != 5 {
		t.Fatalf("len = %d, %#v", len(got), got)
	}
	if got[0].Assistant == nil || got[1].ToolCallID != "call_a" || got[1].Content != "ok" {
		t.Fatalf("prefix = %#v %#v", got[0], got[1])
	}
	if got[2].ToolCallID != "call_b" || got[2].Content != "No result provided" || !got[2].IsError {
		t.Fatalf("synthetic = %#v", got[2])
	}
	if got[3].Role != RoleSystem || got[3].Content != "mid-system" || got[4].Content != "next" {
		t.Fatalf("tail = %#v %#v", got[3], got[4])
	}
}

func TestSanitizeDowngradesImagesForTextModels(t *testing.T) {
	const provider = "sanitize-test"
	models.RegisterProvider(models.ProviderSpec{
		ID: provider,
		Models: []models.Model{
			{ID: "text-only", Input: []string{"text"}},
			{ID: "vision", Input: []string{"text", "image"}},
			{ID: "unspecified"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider(provider) })

	toolRaw := `{"content":[{"type":"text","text":"see"},{"type":"image","data":"AAA","mimeType":"image/png"},{"type":"image","data":"BBB","mimeType":"image/png"},{"type":"text","text":"after"},{"type":"image","data":"CCC","mimeType":"image/jpeg"}]}`
	user := Message{
		Role:    RoleUser,
		Content: "look",
		Images: []ImageContent{
			{Type: "image", Data: "AAA", MimeType: "image/png"},
			{Type: "image", Data: "BBB", MimeType: "image/png"},
		},
	}
	tool := Message{Role: RoleToolResult, ToolCallID: "tu_1", ToolName: "read", Content: toolRaw}
	imagesOnly := Message{
		Role: RoleToolResult, ToolCallID: "tu_2", Content: "note",
		Images: []ImageContent{{Type: "image", Data: "DDD", MimeType: "image/png"}},
	}
	blank := Message{
		Role: RoleUser,
		Images: []ImageContent{
			{Type: "image", Data: "EEE", MimeType: "image/png"},
			{Type: "image", Data: "FFF", MimeType: "image/gif"},
		},
	}
	in := []Message{user, tool, imagesOnly, blank}

	got := transformMessages(in, replayTarget{Provider: provider, Model: "text-only"}, nil)
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	if len(got[0].Images) != 0 || got[0].Content != "look\n"+userImagePlaceholder {
		t.Fatalf("user = %#v", got[0])
	}
	if len(got[3].Images) != 0 || got[3].Content != userImagePlaceholder {
		t.Fatalf("blank user = %#v", got[3])
	}
	if strings.Count(got[0].Content, userImagePlaceholder) != 1 {
		t.Fatalf("user placeholders = %q", got[0].Content)
	}
	if strings.Contains(got[1].Content, `"type":"image"`) || strings.Count(got[1].Content, toolImagePlaceholder) != 2 {
		t.Fatalf("tool json = %q", got[1].Content)
	}
	if !strings.Contains(got[1].Content, "see") || !strings.Contains(got[1].Content, "after") || len(got[1].Images) != 0 {
		t.Fatalf("tool = %#v", got[1])
	}
	if len(got[2].Images) != 0 || got[2].Content != "note\n"+toolImagePlaceholder {
		t.Fatalf("images-only tool = %#v", got[2])
	}
	if len(user.Images) != 2 || user.Content != "look" || len(blank.Images) != 2 || !strings.Contains(tool.Content, `"type":"image"`) {
		t.Fatal("input messages changed")
	}

	vision := transformMessages(in, replayTarget{Provider: provider, Model: "vision"}, nil)
	if len(vision[0].Images) != 2 || vision[0].Content != "look" || len(vision[3].Images) != 2 || strings.Contains(vision[1].Content, toolImagePlaceholder) {
		t.Fatalf("vision downgraded: %#v", vision)
	}
	plain := transformMessages(in, replayTarget{Provider: provider, Model: "unspecified"}, nil)
	if len(plain[0].Images) != 2 || strings.Contains(plain[1].Content, toolImagePlaceholder) {
		t.Fatalf("unspecified input downgraded: %#v", plain)
	}
}

func TestTransformRemapsSyntheticToolCallID(t *testing.T) {
	asst := &AssistantMessage{
		Provider: "openai", API: "openai-completions", Model: "gpt-4o",
		StopReason: StopToolUse,
		Content:    []*Content{{Type: KindToolCall, ToolID: "call|1", ToolName: "read", Arguments: map[string]any{"p": "a"}}},
	}
	got := transformMessages([]Message{{Role: RoleAssistant, Assistant: asst}}, replayTarget{
		Provider: "anthropic", API: "anthropic-messages", Model: "claude-sonnet-4",
	}, normalizeSanitizedToolCallID)
	if len(got) != 2 || got[1].Content != "No result provided" || got[1].ToolCallID != "call_1" {
		t.Fatalf("got = %#v", got)
	}
	if asst.Content[0].ToolID != "call|1" {
		t.Fatal("input tool id changed")
	}
}

func TestPigoMessagesSanitizesWithoutStrippingThinking(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(pigoMessagesFixture))
	}))
	t.Cleanup(srv.Close)

	kept := &AssistantMessage{
		Provider: "anthropic", API: "anthropic-messages", Model: "claude",
		StopReason: StopStop,
		Content:    []*Content{{Type: KindThinking, Thinking: "plan", ThinkingSignature: "sig-keep"}},
	}
	aborted := &AssistantMessage{
		StopReason: StopAborted,
		Content: []*Content{
			{Type: KindText, Text: "partial"},
			{Type: KindToolCall, ToolID: "toolu_1", ToolName: "bash"},
		},
	}
	orphan := &AssistantMessage{
		StopReason: StopToolUse,
		Content:    []*Content{{Type: KindToolCall, ToolID: "toolu_2", ToolName: "read"}},
	}
	client := &PigoMessagesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{Messages: []Message{
		{Role: RoleAssistant, Assistant: kept},
		{Role: RoleAssistant, Assistant: aborted},
		{Role: RoleAssistant, Assistant: orphan},
	}}, Options{Provider: "radius", Model: "qwen"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = stream.Collect()
	raw := string(body)
	if strings.Contains(raw, "partial") || strings.Contains(raw, "toolu_1") {
		t.Fatalf("aborted assistant replayed: %s", raw)
	}
	if !strings.Contains(raw, "sig-keep") || !strings.Contains(raw, "toolu_2") || !strings.Contains(raw, "No result provided") {
		t.Fatalf("body = %s", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
}

func countContent(msgs []Message, text string) int {
	n := 0
	for _, m := range msgs {
		if m.Content == text {
			n++
		}
	}
	return n
}

func countToolResults(msgs []Message, id string) int {
	n := 0
	for _, m := range msgs {
		if m.Assistant == nil && m.ToolCallID == id {
			n++
		}
	}
	return n
}

func indexContent(msgs []Message, text string) int {
	for i, m := range msgs {
		if m.Content == text {
			return i
		}
	}
	return -1
}

func indexToolResult(msgs []Message, id string) int {
	for i, m := range msgs {
		if m.ToolCallID == id {
			return i
		}
	}
	return -1
}
