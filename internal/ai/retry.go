package ai

import "regexp"

// Ported from pi packages/ai/src/utils/retry.ts (isRetryableAssistantError).

func buildProviderErrorPattern(patterns []string) *regexp.Regexp {
	return regexp.MustCompile("(?i)" + joinPattern(patterns))
}

func joinPattern(patterns []string) string {
	out := ""
	for i, p := range patterns {
		if i > 0 {
			out += "|"
		}
		out += p
	}
	return out
}

var nonRetryableProviderLimitError = buildProviderErrorPattern([]string{
	"GoUsageLimitError",
	"FreeUsageLimitError",
	"Monthly usage limit reached",
	"available balance",
	"insufficient_quota",
	"out of budget",
	"quota exceeded",
	"billing",
})

var retryableProviderError = buildProviderErrorPattern([]string{
	"overloaded",
	"rate.?limit",
	"too many requests",
	"429",
	"500",
	"502",
	"503",
	"504",
	"524",
	"service.?unavailable",
	"server.?error",
	"internal.?error",
	"provider.?returned.?error",
	"exceeded request buffer limit while retrying upstream",
	"network.?error",
	"connection.?error",
	"connection.?refused",
	"connection.?lost",
	"other side closed",
	"fetch failed",
	"getaddrinfo",
	"ENOTFOUND",
	"EAI_AGAIN",
	"upstream.?connect",
	"reset before headers",
	"socket hang up",
	"socket connection was closed",
	"timed? out",
	"timeout",
	"terminated",
	"websocket.?closed",
	"websocket.?error",
	"ended without",
	"stream ended before message_stop",
	"stream ended before a terminal response event",
	"http2 request did not get a response",
	"retry delay",
	"you can retry your request",
	"try your request again",
	"please retry your request",
	"ResourceExhausted",
})

// IsRetryableAssistantError classifies whether a failed assistant message looks
// like a transient provider or transport error.
func IsRetryableAssistantError(message *AssistantMessage) bool {
	if message == nil || message.StopReason != StopError || message.ErrorMessage == "" {
		return false
	}
	if nonRetryableProviderLimitError.MatchString(message.ErrorMessage) {
		return false
	}
	return retryableProviderError.MatchString(message.ErrorMessage)
}

// IsRetryableError is the session-level check: overflow is handled by
// compaction, not retry (pi AgentSession._isRetryableError).
func IsRetryableError(message *AssistantMessage, contextWindow int) bool {
	if message == nil {
		return false
	}
	if IsContextOverflow(message, contextWindow) {
		return false
	}
	return IsRetryableAssistantError(message)
}
