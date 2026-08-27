package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
)

// RestoreAIMessages rebuilds provider-facing ai.Messages from session entries,
// rehydrating assistant tool-call blocks and toolResult pairing.
func RestoreAIMessages(entries []Entry) []ai.Message {
	out := make([]ai.Message, 0, len(entries))
	for _, e := range entries {
		if e.Type == "custom" {
			continue
		}
		if e.Type == "custom_message" {
			text := customMessageText(e)
			if text != "" {
				out = append(out, ai.Message{Role: ai.RoleUser, Content: text})
			}
			continue
		}
		if e.Type == "branch_summary" {
			text := e.Summary
			if text != "" {
				out = append(out, ai.Message{Role: ai.RoleUser, Content: compaction.BranchSummaryPrefix + text + compaction.BranchSummarySuffix})
			}
			continue
		}
		if e.Type == "compaction" {
			text := e.Summary
			if text == "" {
				var p struct {
					Summary string `json:"summary"`
				}
				_ = json.Unmarshal(e.Message, &p)
				text = p.Summary
			}
			if text != "" {
				out = append(out, ai.Message{Role: ai.RoleUser, Content: compaction.SummaryPrefix + text + compaction.SummarySuffix})
			}
			continue
		}
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
		case "bashExecution":
			exclude, _ := payload["excludeFromContext"].(bool)
			if exclude {
				continue
			}
			command, _ := payload["command"].(string)
			content, _ := payload["output"].(string)
			cancelled, _ := payload["cancelled"].(bool)
			truncated, _ := payload["truncated"].(bool)
			fullPath, _ := payload["fullOutputPath"].(string)
			var exitCode *int
			switch v := payload["exitCode"].(type) {
			case float64:
				n := int(v)
				exitCode = &n
			case int:
				exitCode = &v
			}
			out = append(out, ai.Message{Role: ai.RoleUser, Content: BashContextText(command, content, cancelled, exitCode, truncated, fullPath)})
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
			content, images := parseUserContent(payload["content"])
			out = append(out, ai.Message{Role: ai.RoleUser, Content: content, Images: images})
		}
	}
	return out
}

func parseUserContent(v any) (string, []ai.ImageContent) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return "", nil
	}
	var text string
	var images []ai.ImageContent
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		switch m["type"] {
		case "text":
			t, _ := m["text"].(string)
			text += t
		case "image":
			data, _ := m["data"].(string)
			mime, _ := m["mimeType"].(string)
			images = append(images, ai.ImageContent{Type: "image", Data: data, MimeType: mime})
		}
	}
	return text, images
}

func customMessageText(e Entry) string {
	if e.Summary != "" {
		return e.Summary
	}
	if len(e.Content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(e.Content, &s) == nil {
		return s
	}
	text, _ := parseUserContent(func() any {
		var v any
		_ = json.Unmarshal(e.Content, &v)
		return v
	}())
	return text
}

// FindByID opens the session whose id equals or is prefixed by id for cwd.
func FindByID(cwd, agentDir, id string) (*Manager, error) {
	return FindByIDAt(cwd, agentDir, id, "")
}

// FindByIDAt is FindByID using an optional session directory override.
func FindByIDAt(cwd, agentDir, id, sessionDir string) (*Manager, error) {
	if id == "" {
		return nil, os.ErrNotExist
	}
	if strings.HasSuffix(id, ".jsonl") || strings.Contains(id, string(filepath.Separator)) {
		return Open(id)
	}
	paths, err := listSessionFilesAt(cwd, agentDir, sessionDir)
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
