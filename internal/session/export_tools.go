package session

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Lowpower/pigo/internal/theme"
)

var templateRenderedTools = map[string]bool{
	"bash": true, "read": true, "write": true, "edit": true, "ls": true,
}

// ToolHTMLRenderer pre-renders custom tool calls/results for HTML export.
type ToolHTMLRenderer interface {
	RenderCall(toolCallID, toolName string, args any) string
	RenderResult(toolCallID, toolName string, result []map[string]any, details any, isError bool) (collapsed, expanded string)
}

// RenderedToolHTML is SESSION_DATA.renderedTools[id].
type RenderedToolHTML struct {
	CallHTML            string `json:"callHtml,omitempty"`
	ResultHTMLCollapsed string `json:"resultHtmlCollapsed,omitempty"`
	ResultHTMLExpanded  string `json:"resultHtmlExpanded,omitempty"`
}

func preRenderCustomTools(entries []Entry, renderer ToolHTMLRenderer) map[string]RenderedToolHTML {
	if renderer == nil {
		return nil
	}
	out := map[string]RenderedToolHTML{}
	for _, e := range entries {
		if e.Type != "message" && e.Type != "" {
			continue
		}
		var msg map[string]any
		if json.Unmarshal(e.Message, &msg) != nil {
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "assistant":
			blocks, _ := msg["content"].([]any)
			for _, raw := range blocks {
				block, _ := raw.(map[string]any)
				if block == nil {
					continue
				}
				typ, _ := block["type"].(string)
				if typ != "toolCall" {
					continue
				}
				name, _ := block["name"].(string)
				if templateRenderedTools[name] {
					continue
				}
				id, _ := block["id"].(string)
				if id == "" {
					continue
				}
				html := renderer.RenderCall(id, name, block["arguments"])
				if html == "" {
					continue
				}
				cur := out[id]
				cur.CallHTML = html
				out[id] = cur
			}
		case "toolResult", "tool":
			id, _ := msg["toolCallId"].(string)
			name, _ := msg["toolName"].(string)
			if templateRenderedTools[name] && out[id].CallHTML == "" {
				continue
			}
			isError, _ := msg["isError"].(bool)
			collapsed, expanded := renderer.RenderResult(id, name, toolResultBlocks(msg["content"]), msg["details"], isError)
			if collapsed == "" && expanded == "" {
				continue
			}
			cur := out[id]
			cur.ResultHTMLCollapsed = collapsed
			cur.ResultHTMLExpanded = expanded
			out[id] = cur
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toolResultBlocks(content any) []map[string]any {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []map[string]any{{"type": "text", "text": v}}
	case []any:
		var out []map[string]any
		for _, item := range v {
			m, _ := item.(map[string]any)
			if m != nil {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func toolResultText(blocks []map[string]any) string {
	var b strings.Builder
	for _, m := range blocks {
		if m["type"] == "text" {
			t, _ := m["text"].(string)
			b.WriteString(t)
		}
	}
	return b.String()
}

func trimRenderedLines(lines []string) []string {
	start, end := 0, len(lines)
	blank := func(s string) bool {
		return strings.TrimSpace(ansiSGRRe.ReplaceAllString(s, "")) == ""
	}
	for start < end && blank(lines[start]) {
		start++
	}
	for end > start && blank(lines[end-1]) {
		end--
	}
	return lines[start:end]
}

func themeColor(th theme.Theme, token, fallback string) string {
	if th.Colors != nil {
		if v := th.Colors[token]; v != "" {
			return v
		}
	}
	return fallback
}

func paint(color, text string) string {
	color = strings.TrimSpace(color)
	if color == "" || text == "" {
		return text
	}
	if strings.HasPrefix(color, "#") && len(color) == 7 {
		r, err1 := strconv.ParseInt(color[1:3], 16, 0)
		g, err2 := strconv.ParseInt(color[3:5], 16, 0)
		b, err3 := strconv.ParseInt(color[5:7], 16, 0)
		if err1 == nil && err2 == nil && err3 == nil {
			return "\x1b[38;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b)) + "m" + text + "\x1b[0m"
		}
	}
	if n, err := strconv.Atoi(color); err == nil {
		return "\x1b[38;5;" + strconv.Itoa(n) + "m" + text + "\x1b[0m"
	}
	return text
}

func paintBold(color, text string) string {
	return paint(color, "\x1b[1m"+text+"\x1b[22m")
}
