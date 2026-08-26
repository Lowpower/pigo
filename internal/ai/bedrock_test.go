package ai

import (
	"testing"

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
	}})
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
