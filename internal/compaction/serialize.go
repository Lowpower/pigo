package compaction

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
)

// toolResultMaxChars is the per-result cap in a serialized summary transcript.
const toolResultMaxChars = 2000

// SummarizationSystemPrompt tells the model the transcript is not a conversation to continue.
const SummarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

// serializeConversation renders messages as plain text so the summarizer cannot
// treat them as a live conversation. Empty sections are omitted. Tool results
// longer than toolResultMaxChars keep their prefix and record the cut length.
func serializeConversation(msgs []ai.Message) string {
	var parts []string
	for _, msg := range msgs {
		switch msg.Role {
		case ai.RoleUser:
			if text := userText(msg); text != "" {
				parts = append(parts, "[User]: "+text)
			}
		case ai.RoleAssistant:
			parts = append(parts, assistantParts(msg)...)
		case ai.RoleToolResult, ai.RoleTool:
			if text := msg.Content; text != "" {
				parts = append(parts, "[Tool result]: "+truncateForSummary(text, toolResultMaxChars))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func userText(msg ai.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	return msg.Text()
}

func assistantParts(msg ai.Message) []string {
	var thinking []string
	var toolCalls []string
	var hasText bool
	var text string
	if msg.Assistant != nil {
		for _, block := range msg.Assistant.Content {
			if block == nil {
				continue
			}
			switch block.Type {
			case ai.KindThinking:
				if block.Thinking != "" {
					thinking = append(thinking, block.Thinking)
				}
			case ai.KindText:
				hasText = true
				text += block.Text
			case ai.KindToolCall:
				toolCalls = append(toolCalls, formatToolCall(block))
			}
		}
	}
	if !hasText && msg.Content != "" {
		hasText = true
		text = msg.Content
	}
	var parts []string
	if len(thinking) > 0 {
		parts = append(parts, "[Assistant thinking]: "+strings.Join(thinking, "\n"))
	}
	if hasText {
		parts = append(parts, "[Assistant]: "+text)
	}
	if len(toolCalls) > 0 {
		parts = append(parts, "[Assistant tool calls]: "+strings.Join(toolCalls, "; "))
	}
	return parts
}

func formatToolCall(block *ai.Content) string {
	name := block.ToolName
	if name == "" {
		name = block.ToolID
	}
	if len(block.Arguments) == 0 {
		return name + "()"
	}
	keys := make([]string, 0, len(block.Arguments))
	for key := range block.Arguments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys))
	for _, key := range keys {
		encoded, err := json.Marshal(block.Arguments[key])
		if err != nil {
			encoded = []byte(fmt.Sprint(block.Arguments[key]))
		}
		args = append(args, key+"="+string(encoded))
	}
	return name + "(" + strings.Join(args, ", ") + ")"
}

func truncateForSummary(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	cut := len(runes) - maxChars
	return string(runes[:maxChars]) + "\n\n[... " + strconv.Itoa(cut) + " more characters truncated]"
}
