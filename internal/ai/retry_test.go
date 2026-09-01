package ai

import "testing"

func errorAssistant(msg string) *AssistantMessage {
	return &AssistantMessage{Role: RoleAssistant, StopReason: StopError, ErrorMessage: msg}
}

func TestIsRetryableAssistantError(t *testing.T) {
	t.Parallel()
	retryable := []string{
		"overloaded_error",
		"524 status code (no body)",
		"An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID req_******** in your message.",
		`{"message":"The system encountered an unexpected error during processing. Try your request again."}`,
		"ResourceExhausted: Worker local total request limit reached (288/48)",
		"The socket connection was closed unexpectedly. For more information, pass `verbose: true` in the second argument to fetch()",
		"Error: exceeded request buffer limit while retrying upstream",
		"The pending stream has been canceled (caused by: getaddrinfo ENOTFOUND bedrock-runtime.us-east-1.amazonaws.com)",
		"connect ENOTFOUND api.example.com",
		"EAI_AGAIN api.example.com",
		"getaddrinfo failed for api.example.com",
		"OpenAI Responses stream ended before a terminal response event",
		"429 too many requests",
		"network_error",
		"Provider finish_reason: network_error",
	}
	for _, msg := range retryable {
		if !IsRetryableAssistantError(errorAssistant(msg)) {
			t.Errorf("want retryable: %q", msg)
		}
	}

	non := []string{
		"insufficient_quota",
		"429 quota exceeded",
		"invalid_api_key",
		"Monthly usage limit reached",
		"out of budget",
		"GoUsageLimitError",
	}
	for _, msg := range non {
		if IsRetryableAssistantError(errorAssistant(msg)) {
			t.Errorf("want non-retryable: %q", msg)
		}
	}

	if IsRetryableAssistantError(&AssistantMessage{Role: RoleAssistant, StopReason: StopStop, Content: []*Content{{Type: KindText, Text: "not an error"}}}) {
		t.Fatal("successful stop should not be retryable")
	}
	if IsRetryableAssistantError(&AssistantMessage{Role: RoleAssistant, StopReason: StopError}) {
		t.Fatal("error without message should not be retryable")
	}
}

func TestIsContextOverflow(t *testing.T) {
	t.Parallel()
	if !IsContextOverflow(errorAssistant("prompt is too long: 213462 tokens > 200000 maximum"), 0) {
		t.Fatal("anthropic overflow should match")
	}
	if IsContextOverflow(errorAssistant("Throttling error: Too many tokens, please wait"), 0) {
		t.Fatal("throttling should not be overflow")
	}
	if IsContextOverflow(errorAssistant("overloaded_error"), 0) {
		t.Fatal("overloaded should not be overflow")
	}
}

func TestIsRecoverableLength(t *testing.T) {
	msg := &AssistantMessage{StopReason: StopLength, Usage: Usage{Output: 10}}
	if !IsRecoverableLength(msg, 128000) {
		t.Fatal("want recoverable")
	}
	if IsRecoverableLength(msg, 10) || IsRecoverableLength(msg, 0) {
		t.Fatal("at/zero limit is not recoverable")
	}
	if IsRecoverableLength(&AssistantMessage{StopReason: StopStop, Usage: Usage{Output: 1}}, 100) {
		t.Fatal("stop is not recoverable")
	}
}
