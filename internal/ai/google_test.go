package ai

import "testing"

func TestGoogleContentsRolesAndTools(t *testing.T) {
	got := googleContents(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindText, Text: "yo"},
			{Type: KindToolCall, ToolID: "1", ToolName: "read", Arguments: map[string]any{"path": "a"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "1", ToolName: "read", Content: "ok"},
	}})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Role != "user" || got[1].Role != "model" || got[2].Role != "user" {
		t.Fatalf("roles = %q %q %q", got[0].Role, got[1].Role, got[2].Role)
	}
	if got[1].Parts[1].FunctionCall == nil || got[1].Parts[1].FunctionCall.Name != "read" {
		t.Fatalf("tool part = %+v", got[1].Parts)
	}
	if got[2].Parts[0].FunctionResponse == nil {
		t.Fatal("missing function response")
	}
}
