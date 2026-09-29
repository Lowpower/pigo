package bugreport

import (
	"regexp"

	"github.com/Lowpower/pigo/internal/ai"
)

var abortText = regexp.MustCompile(`(?i)\b(?:abort(?:ed)?|cancel(?:l?ed)?)\b`)

// ShouldSuggestBug reports whether a failed assistant turn should mention /bug.
// Retryable provider errors and user cancellations do not.
func ShouldSuggestBug(message *ai.AssistantMessage) bool {
	if message == nil || message.StopReason != ai.StopError {
		return false
	}
	if ai.IsRetryableAssistantError(message) {
		return false
	}
	if abortText.MatchString(message.ErrorMessage) {
		return false
	}
	return true
}
