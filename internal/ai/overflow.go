package ai

import "regexp"

// Ported from pi packages/ai/src/utils/overflow.ts (isContextOverflow).

var overflowPatterns = compileAll([]string{
	`prompt is too long`,
	`request_too_large`,
	`input is too long for requested model`,
	`exceeds the context window`,
	`exceeds (?:the )?(?:model'?s )?maximum context length(?: of [\d,]+ tokens?|\s*\([\d,]+\))`,
	`input token count.*exceeds the maximum`,
	`maximum prompt length is \d+`,
	`reduce the length of the messages`,
	`maximum context length is \d+ tokens`,
	`exceeds (?:the )?maximum allowed input length of [\d,]+ tokens?`,
	`input \(\d+ tokens\) is longer than the model'?s context length \(\d+ tokens\)`,
	`exceeds the limit of \d+`,
	`exceeds the available context size`,
	`greater than the context length`,
	`context window exceeds limit`,
	`exceeded model token limit`,
	`too large for model with \d+ maximum context length`,
	`prompt has [\d,]+ tokens?, but the configured context size is [\d,]+ tokens?`,
	`model_context_window_exceeded`,
	`prompt too long; exceeded (?:max )?context length`,
	`range of input length should be`,
	`context[_ ]length[_ ]exceeded`,
	`too many tokens`,
	`token limit exceeded`,
	`^4(?:00|13)\s*(?:status code)?\s*\(no body\)`,
})

var nonOverflowPatterns = compileAll([]string{
	`^(Throttling error|Service unavailable):`,
	`rate limit`,
	`too many requests`,
})

func compileAll(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile("(?i)" + p)
	}
	return out
}

// IsContextOverflow reports whether an assistant message is a context overflow.
func IsContextOverflow(message *AssistantMessage, contextWindow int) bool {
	if message == nil {
		return false
	}
	if message.StopReason == StopError && message.ErrorMessage != "" {
		non := false
		for _, p := range nonOverflowPatterns {
			if p.MatchString(message.ErrorMessage) {
				non = true
				break
			}
		}
		if !non {
			for _, p := range overflowPatterns {
				if p.MatchString(message.ErrorMessage) {
					return true
				}
			}
		}
	}
	if contextWindow > 0 && message.StopReason == StopStop {
		inputTokens := message.Usage.Input + message.Usage.CacheRead
		if inputTokens > contextWindow {
			return true
		}
	}
	if contextWindow > 0 && message.StopReason == StopLength && message.Usage.Output == 0 {
		inputTokens := message.Usage.Input + message.Usage.CacheRead
		if inputTokens >= int(float64(contextWindow)*0.99) {
			return true
		}
	}
	return false
}
