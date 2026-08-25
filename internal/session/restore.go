package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
)

// RestoreAIMessages rebuilds provider-facing ai.Messages from session entries,
// rehydrating assistant tool-call blocks and toolResult pairing.
func RestoreAIMessages(entries []Entry) []ai.Message {
	out := make([]ai.Message, 0, len(entries))
	for _, e := range entries {
		if e.Type != "message" && e.Type != "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(e.Message, &payload); err != nil {
			continue
		}
		role, _ := payload["role"].(string)
		switch role {
		case "assistant":
			var am ai.AssistantMessage
			if err := json.Unmarshal(e.Message, &am); err == nil && (len(am.Content) > 0 || am.Role != "") {
				out = append(out, ai.Message{Role: ai.RoleAssistant, Content: am.Text(), Assistant: &am})
				continue
			}
			content, _ := payload["content"].(string)
			out = append(out, ai.Message{Role: ai.RoleAssistant, Content: content})
		case "toolResult", "tool":
			content, _ := payload["content"].(string)
			id, _ := payload["toolCallId"].(string)
			if id == "" {
				id, _ = payload["tool_call_id"].(string)
			}
			name, _ := payload["toolName"].(string)
			errFlag, _ := payload["isError"].(bool)
			out = append(out, ai.Message{Role: ai.RoleToolResult, Content: content, ToolCallID: id, ToolName: name, IsError: errFlag})
		default:
			content, _ := payload["content"].(string)
			out = append(out, ai.Message{Role: ai.RoleUser, Content: content})
		}
	}
	return out
}

// FindByID opens the session whose id equals or is prefixed by id for cwd.
func FindByID(cwd, agentDir, id string) (*Manager, error) {
	if id == "" {
		return nil, os.ErrNotExist
	}
	if strings.HasSuffix(id, ".jsonl") || strings.Contains(id, string(filepath.Separator)) {
		return Open(id)
	}
	paths, err := listSessionFiles(cwd, agentDir)
	if err != nil {
		return nil, err
	}
	for _, pth := range paths {
		h, _, err := Load(pth)
		if err != nil {
			continue
		}
		if h.ID == id || strings.HasPrefix(h.ID, id) || strings.Contains(filepath.Base(pth), id) {
			return Open(pth)
		}
	}
	return nil, os.ErrNotExist
}
