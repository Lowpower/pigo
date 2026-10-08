package ai

import (
	"math"
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
	}}, replayTarget{})
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
