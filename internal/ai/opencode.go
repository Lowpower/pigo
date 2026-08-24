package ai

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

// defaultOpenCodeBaseURL is the OpenCode Zen gateway base. The adapters append
// /v1/messages (Anthropic) or /v1/chat/completions (OpenAI-compatible).
const defaultOpenCodeBaseURL = "https://opencode.ai/zen"

// NewOpenCodeFromEnv builds a StreamFn for the OpenCode gateway from
// OPENCODE_API_KEY (required) and OPENCODE_BASE_URL (optional; defaults to
// OpenCode Zen; set it to the OpenCode Go base for that plan).
//
// OpenCode multiplexes several wire formats behind one key, so this routes each
// request by model id: claude-* -> Anthropic Messages (x-api-key), everything
// else -> OpenAI Chat Completions (Bearer). GPT models that require the OpenAI
// Responses API are not yet supported (that adapter is on the backlog).
func NewOpenCodeFromEnv() (StreamFn, bool) {
	key := os.Getenv("OPENCODE_API_KEY")
	if key == "" {
		return nil, false
	}
	base := os.Getenv("OPENCODE_BASE_URL")
	if base == "" {
		base = defaultOpenCodeBaseURL
	}
	base = strings.TrimRight(base, "/")
	httpClient := &http.Client{Timeout: 5 * time.Minute}

	anthropic := (&AnthropicClient{BaseURL: base, APIKey: key, HTTPClient: httpClient}).StreamFn()
	openai := (&OpenAICompletionsClient{BaseURL: base, APIKey: key, HTTPClient: httpClient}).StreamFn()

	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		if isAnthropicModel(opts.Model) {
			return anthropic(ctx, reqCtx, opts)
		}
		return openai(ctx, reqCtx, opts)
	}, true
}

// isAnthropicModel reports whether a model id uses the Anthropic Messages wire
// format (Claude models on the gateway).
func isAnthropicModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "claude")
}
