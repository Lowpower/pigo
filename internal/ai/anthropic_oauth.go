package ai

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Claude Code client version sent on Anthropic subscription OAuth requests.
// The OAuth endpoint rejects identities that report a stale client version.
const claudeCodeVersion = "2.1.280"

const claudeCodeIdentity = "You are Claude Code, Anthropic's official CLI for Claude."

// claudeCodeNames maps a lowercased tool name to the casing Claude Code sends.
var claudeCodeNames = func() map[string]string {
	canonical := []string{
		"Read",
		"Write",
		"Edit",
		"Bash",
		"Grep",
		"Glob",
		"AskUserQuestion",
		"EnterPlanMode",
		"ExitPlanMode",
		"KillShell",
		"NotebookEdit",
		"Skill",
		"Task",
		"TaskOutput",
		"TodoWrite",
		"WebFetch",
		"WebSearch",
	}
	out := make(map[string]string, len(canonical))
	for _, name := range canonical {
		out[strings.ToLower(name)] = name
	}
	return out
}()

func anthropicOAuth(apiKey string, headers map[string]string) bool {
	if isAnthropicOAuthToken(apiKey) {
		return true
	}
	authz, _ := lookupHeader(headers, "Authorization")
	return isAnthropicOAuthToken(strings.TrimPrefix(authz, "Bearer "))
}

func applyClaudeCodeHeaders(h http.Header) {
	if h.Get("User-Agent") == "" {
		h.Set("User-Agent", "claude-cli/"+claudeCodeVersion)
	}
	if h.Get("x-app") == "" {
		h.Set("x-app", "cli")
	}
	if h.Get("anthropic-dangerous-direct-browser-access") == "" {
		h.Set("anthropic-dangerous-direct-browser-access", "true")
	}
}

func toClaudeCodeName(name string) string {
	if canonical, ok := claudeCodeNames[strings.ToLower(name)]; ok {
		return canonical
	}
	return name
}

func fromClaudeCodeName(name string, tools []Tool) string {
	lower := strings.ToLower(name)
	for _, tool := range tools {
		if strings.ToLower(tool.Name) == lower {
			return tool.Name
		}
	}
	return name
}

// applyClaudeCodeWire prepends the Claude Code identity system block and
// rewrites known tool names on an Anthropic Messages request body.
func applyClaudeCodeWire(body []byte, opts Options) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	prependClaudeCodeIdentity(req, opts)
	rewriteClaudeCodeToolNames(req)
	return json.Marshal(req)
}

func prependClaudeCodeIdentity(req map[string]any, opts Options) {
	cc := anthropicCacheControl(opts)
	identity := map[string]any{
		"type": "text",
		"text": claudeCodeIdentity,
	}
	if cc != nil {
		identity["cache_control"] = cc
	}
	existing, _ := req["system"].([]any)
	req["system"] = append([]any{identity}, existing...)
}

func rewriteClaudeCodeToolNames(req map[string]any) {
	for _, raw := range jsonArray(req["tools"]) {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := tool["name"].(string); ok {
			tool["name"] = toClaudeCodeName(name)
		}
	}
	for _, raw := range jsonArray(req["messages"]) {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rewriteClaudeCodeContent(msg["content"])
	}
}

func rewriteClaudeCodeContent(content any) {
	for _, raw := range jsonArray(content) {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "tool_use":
			if name, ok := block["name"].(string); ok {
				block["name"] = toClaudeCodeName(name)
			}
		case "tool_reference":
			if name, ok := block["tool_name"].(string); ok {
				block["tool_name"] = toClaudeCodeName(name)
			}
		case "tool_result":
			rewriteClaudeCodeContent(block["content"])
		}
	}
}

func jsonArray(v any) []any {
	s, _ := v.([]any)
	return s
}
