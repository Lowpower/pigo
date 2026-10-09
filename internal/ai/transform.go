package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// replayTarget is the provider, API, and model a request is about to call.
type replayTarget struct {
	Provider string
	API      string
	Model    string
}

// toolCallIDNormalizer rewrites a tool-call id for the target API.
// source is the assistant message that produced the id.
type toolCallIDNormalizer func(id string, source *AssistantMessage) string

// recordedProvider is the catalog provider id to store on an assistant message.
// An empty Options.Provider keeps the adapter's own id.
func recordedProvider(opts Options, fallback string) string {
	if p := strings.TrimSpace(opts.Provider); p != "" {
		return p
	}
	return fallback
}

// transformMessages rewrites a copy of msgs for the target model.
// It first drops aborted turns, closes orphan tool calls, and downgrades
// images the target model cannot accept. Assistant messages whose provider,
// api, and model all match keep thinking blocks and tool-call ids. Anything
// else drops signatures: thinking text becomes a plain text block, redacted
// thinking is discarded, and tool-call ids are rewritten with normalize when
// it is set. Tool results that point at a rewritten id are updated to match.
// The input messages are not modified.
func transformMessages(msgs []Message, target replayTarget, normalize toolCallIDNormalizer) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	msgs = sanitizeProviderMessages(msgs, modelSupportsImages(target.Provider, target.Model))
	out := make([]Message, len(msgs))
	idMap := map[string]string{}
	for i, m := range msgs {
		out[i] = m
		if m.Assistant != nil {
			out[i].Assistant = transformAssistant(m.Assistant, target, normalize, idMap)
			continue
		}
		if m.ToolCallID == "" {
			continue
		}
		if next, ok := idMap[m.ToolCallID]; ok && next != m.ToolCallID {
			out[i].ToolCallID = next
		}
	}
	return out
}

func transformAssistant(m *AssistantMessage, target replayTarget, normalize toolCallIDNormalizer, idMap map[string]string) *AssistantMessage {
	cp := *m
	same := m.Provider == target.Provider && m.API == target.API && m.Model == target.Model
	content := make([]*Content, 0, len(m.Content))
	for _, c := range m.Content {
		if c == nil {
			continue
		}
		switch c.Type {
		case KindThinking:
			if c.Redacted {
				if same {
					content = append(content, cloneContent(c))
				}
				continue
			}
			if same && strings.TrimSpace(c.ThinkingSignature) != "" {
				content = append(content, cloneContent(c))
				continue
			}
			if strings.TrimSpace(c.Thinking) == "" {
				continue
			}
			if same {
				content = append(content, cloneContent(c))
				continue
			}
			content = append(content, &Content{Type: KindText, Text: c.Thinking})
		case KindText:
			block := cloneContent(c)
			if !same {
				block.TextSignature = ""
			}
			content = append(content, block)
		case KindToolCall:
			block := cloneContent(c)
			if !same {
				block.ThinkingSignature = ""
				if normalize != nil && block.ToolID != "" {
					next := normalize(c.ToolID, m)
					if next != "" && next != c.ToolID {
						idMap[c.ToolID] = next
						block.ToolID = next
					}
				}
			}
			content = append(content, block)
		default:
			content = append(content, cloneContent(c))
		}
	}
	cp.Content = content
	return &cp
}

func cloneContent(c *Content) *Content {
	cp := *c
	if c.Arguments != nil {
		cp.Arguments = make(map[string]any, len(c.Arguments))
		for k, v := range c.Arguments {
			cp.Arguments[k] = v
		}
	}
	return &cp
}

func sanitizeToolCallID(id string, maxLen int) string {
	if id == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if maxLen > 0 && len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

func normalizeSanitizedToolCallID(id string, _ *AssistantMessage) string {
	return sanitizeToolCallID(id, 64)
}

func normalizeOpenAIChatToolCallID(provider string) toolCallIDNormalizer {
	return func(id string, _ *AssistantMessage) string {
		if i := strings.Index(id, "|"); i >= 0 {
			callID := sanitizeToolCallID(id[:i], 0)
			itemID := sanitizeToolCallID(id[i+1:], 0)
			combined := callID
			if itemID != "" {
				combined = callID + "_" + itemID
			}
			if len(combined) <= 40 {
				return combined
			}
			hash := shortHash8(id)
			prefixLen := 40 - len(hash) - 1
			if prefixLen < 1 {
				prefixLen = 1
			}
			if len(callID) > prefixLen {
				callID = callID[:prefixLen]
			}
			if callID == "" {
				return hash
			}
			return callID + "_" + hash
		}
		if provider == "openai" && len(id) > 40 {
			return id[:40]
		}
		return id
	}
}

func normalizeResponsesToolCallID(id string, _ *AssistantMessage) string {
	call, item, ok := strings.Cut(id, "|")
	if !ok {
		return strings.TrimRight(sanitizeToolCallID(id, 64), "_")
	}
	call = strings.TrimRight(sanitizeToolCallID(call, 64), "_")
	item = sanitizeToolCallID(item, 0)
	if !strings.HasPrefix(item, "fc_") {
		item = "fc_" + item
	}
	item = strings.TrimRight(sanitizeToolCallID(item, 64), "_")
	if call == "" {
		return item
	}
	if item == "" {
		return call
	}
	return call + "|" + item
}

func normalizeMistralToolCallID(id string, _ *AssistantMessage) string {
	return mistralToolCallID(id)
}

func normalizeGoogleToolCallID(id, model string) string {
	if !requiresGoogleToolCallID(model) {
		return id
	}
	return sanitizeToolCallID(id, 64)
}

func requiresGoogleToolCallID(modelID string) bool {
	id := strings.ToLower(modelID)
	if strings.HasPrefix(id, "claude-") || strings.HasPrefix(id, "gpt-oss-") {
		return true
	}
	major, ok := geminiMajorVersion(modelID)
	return ok && major >= 3
}

func geminiMajorVersion(modelID string) (int, bool) {
	id := strings.ToLower(modelID)
	var rest string
	switch {
	case strings.HasPrefix(id, "gemini-live-"):
		rest = id[len("gemini-live-"):]
	case strings.HasPrefix(id, "gemini-"):
		rest = id[len("gemini-"):]
	default:
		return 0, false
	}
	if rest == "" || rest[0] < '0' || rest[0] > '9' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			break
		}
		n = n*10 + int(rest[i]-'0')
	}
	return n, true
}

func shortHash8(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}
