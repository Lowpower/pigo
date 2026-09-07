package ai

import (
	"strings"

	"github.com/Lowpower/pigo/internal/models"
)

const (
	midConvoOutputConfigBeta    = "mid-conversation-output-config-2026-07-01"
	thinkingBindingControlsBeta = "thinking-binding-controls-2026-08-01"
)

func lookupCompat(opts Options) *models.Compat {
	if opts.Provider == "" || opts.Model == "" {
		return nil
	}
	m, ok := models.Lookup(opts.Provider, opts.Model)
	if !ok {
		return nil
	}
	return m.Compat
}

func toolStrict(t Tool) bool {
	if t.ConstrainedSampling == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(t.ConstrainedSampling.Strict)) {
	case "prefer", "required", "true", "on":
		return true
	default:
		return false
	}
}

func longCacheOK(c *models.Compat) bool {
	if c == nil || c.SupportsLongCacheRetention == nil {
		return true
	}
	return *c.SupportsLongCacheRetention
}

func midConvoEffort(opts Options) bool {
	c := lookupCompat(opts)
	return c != nil && c.SupportsMidConvoEffort
}

func midConvoBetas(opts Options) []string {
	if !midConvoEffort(opts) {
		return nil
	}
	return []string{midConvoOutputConfigBeta, thinkingBindingControlsBeta}
}

func applyThinkingFormat(req map[string]any, opts Options) {
	c := lookupCompat(opts)
	format := ""
	if c != nil {
		format = strings.ToLower(strings.TrimSpace(c.ThinkingFormat))
	}
	on := reasoningEffort(opts) != ""
	effort := reasoningEffort(opts)
	switch format {
	case "zai":
		delete(req, "reasoning_effort")
		if on {
			req["thinking"] = map[string]any{"type": "enabled", "clear_thinking": false}
			if c != nil && c.SupportsReasoningEffort {
				req["reasoning_effort"] = effort
			}
		} else {
			req["thinking"] = map[string]any{"type": "disabled"}
		}
	case "qwen":
		delete(req, "reasoning_effort")
		req["enable_thinking"] = on
		if on && c != nil && c.SupportsReasoningEffort {
			req["reasoning_effort"] = effort
		}
	case "qwen-chat-template":
		delete(req, "reasoning_effort")
		req["chat_template_kwargs"] = map[string]any{"enable_thinking": on, "preserve_thinking": true}
	case "chat-template":
		delete(req, "reasoning_effort")
		if c != nil {
			if kwargs := resolveTemplateVars(c.ChatTemplateKwargs, on, effort); kwargs != nil {
				req["chat_template_kwargs"] = kwargs
			}
		}
	case "baseten":
		delete(req, "reasoning_effort")
		if c != nil {
			if args := resolveTemplateVars(c.ChatTemplateArgs, on, effort); args != nil {
				req["chat_template_args"] = args
			}
			if c.SupportsReasoningEffort && effort != "" {
				req["reasoning_effort"] = effort
			}
		}
	case "deepseek":
		delete(req, "reasoning_effort")
		if on {
			req["thinking"] = map[string]any{"type": "enabled"}
			if c != nil && c.SupportsReasoningEffort {
				req["reasoning_effort"] = effort
			}
		} else {
			req["thinking"] = map[string]any{"type": "disabled"}
		}
	case "openrouter":
		delete(req, "reasoning_effort")
		if on {
			req["reasoning"] = map[string]any{"effort": effort}
		} else {
			req["reasoning"] = map[string]any{"effort": "none"}
		}
	case "ant-ling":
		delete(req, "reasoning_effort")
		if on {
			req["reasoning"] = map[string]any{"effort": effort}
		}
	case "together":
		delete(req, "reasoning_effort")
		req["reasoning"] = map[string]any{"enabled": on}
		if on && c != nil && c.SupportsReasoningEffort {
			req["reasoning_effort"] = effort
		}
	case "string-thinking":
		delete(req, "reasoning_effort")
		if on {
			req["thinking"] = effort
		} else {
			req["thinking"] = "none"
		}
	}
	if c != nil && c.VLLMPriority != nil {
		req["priority"] = c.VLLMPriority
	}
	if strings.EqualFold(opts.CacheRetention, "long") && longCacheOK(c) {
		req["prompt_cache_retention"] = "24h"
	}
}

func resolveTemplateVars(raw any, thinkingOn bool, effort string) map[string]any {
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	return substTemplateMap(m, thinkingOn, effort)
}

func substTemplateMap(m map[string]any, thinkingOn bool, effort string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = substTemplateValue(v, thinkingOn, effort)
	}
	return out
}

func substTemplateValue(v any, thinkingOn bool, effort string) any {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["$var"].(string); ok && len(t) == 1 {
			switch ref {
			case "thinking.enabled":
				return thinkingOn
			case "thinking.effort":
				return effort
			default:
				return v
			}
		}
		return substTemplateMap(t, thinkingOn, effort)
	default:
		return v
	}
}

func promptCacheOptions(opts Options) map[string]any {
	c := lookupCompat(opts)
	if c == nil || !c.SupportsExplicitPromptCacheMode {
		return nil
	}
	ret := strings.ToLower(strings.TrimSpace(opts.CacheRetention))
	if ret == "none" {
		return map[string]any{"mode": "explicit"}
	}
	if ret == "long" && longCacheOK(c) {
		return map[string]any{"ttl": "30m"}
	}
	return nil
}

func promptCacheRetention24h(opts Options) bool {
	c := lookupCompat(opts)
	if c != nil && c.SupportsExplicitPromptCacheMode {
		return false
	}
	return strings.EqualFold(opts.CacheRetention, "long") && longCacheOK(c)
}
