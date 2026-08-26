package session

import (
	"encoding/json"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
)

// Stats is the RPC get_session_stats payload (pi SessionStats).
// packages/coding-agent/src/core/agent-session.ts
type Stats struct {
	SessionFile       string        `json:"sessionFile,omitempty"`
	SessionID         string        `json:"sessionId"`
	UserMessages      int           `json:"userMessages"`
	AssistantMessages int           `json:"assistantMessages"`
	ToolCalls         int           `json:"toolCalls"`
	ToolResults       int           `json:"toolResults"`
	TotalMessages     int           `json:"totalMessages"`
	Tokens            TokenTotals   `json:"tokens"`
	Cost              float64       `json:"cost"`
	ContextUsage      *ContextUsage `json:"contextUsage,omitempty"`
}

// TokenTotals aggregates billed token counts across the session.
type TokenTotals struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
	Total      int `json:"total"`
}

// ContextUsage is current-window usage (pi ContextUsage).
type ContextUsage struct {
	Tokens        *int     `json:"tokens"`
	ContextWindow int      `json:"contextWindow"`
	Percent       *float64 `json:"percent"`
}

// CollectStats walks session entries. contextMsgs is the live branch used for
// contextUsage (typically RestoreAIMessages). contextWindow 0 omits contextUsage.
func CollectStats(m *Manager, contextMsgs []ai.Message, contextWindow int) Stats {
	s := Stats{}
	if m != nil {
		s.SessionFile = m.File()
		s.SessionID = m.ID()
		for _, e := range m.Entries() {
			if e.Type != "message" && e.Type != "" {
				continue
			}
			s.TotalMessages++
			var payload map[string]any
			if err := json.Unmarshal(e.Message, &payload); err != nil {
				continue
			}
			role, _ := payload["role"].(string)
			switch role {
			case "user":
				s.UserMessages++
			case "toolResult", "tool":
				s.ToolResults++
			case "assistant":
				s.AssistantMessages++
				var am ai.AssistantMessage
				if err := json.Unmarshal(e.Message, &am); err == nil {
					for _, c := range am.Content {
						if c != nil && c.Type == ai.KindToolCall {
							s.ToolCalls++
						}
					}
					s.Tokens.Input += am.Usage.Input
					s.Tokens.Output += am.Usage.Output
					s.Tokens.CacheRead += am.Usage.CacheRead
					s.Tokens.CacheWrite += am.Usage.CacheWrite
				}
			}
		}
	}
	s.Tokens.Total = s.Tokens.Input + s.Tokens.Output + s.Tokens.CacheRead + s.Tokens.CacheWrite
	if contextWindow > 0 {
		tok := compaction.EstimateContextTokens(contextMsgs)
		pct := 0.0
		if contextWindow > 0 {
			pct = float64(tok) / float64(contextWindow) * 100
		}
		s.ContextUsage = &ContextUsage{Tokens: &tok, ContextWindow: contextWindow, Percent: &pct}
	}
	return s
}
