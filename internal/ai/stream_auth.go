package ai

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

// StreamWithAuth builds a StreamFn from resolved provider auth.
func StreamWithAuth(provider, apiKey, baseURL string, headers map[string]string) StreamFn {
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	switch provider {
	case "opencode":
		base := baseURL
		if base == "" {
			base = os.Getenv("OPENCODE_BASE_URL")
		}
		if base == "" {
			base = defaultOpenCodeBaseURL
		}
		base = strings.TrimRight(base, "/")
		anthropic := (&AnthropicClient{BaseURL: base, APIKey: apiKey, Headers: headers, HTTPClient: httpClient}).StreamFn()
		openai := (&OpenAICompletionsClient{BaseURL: base, APIKey: apiKey, Headers: headers, HTTPClient: httpClient}).StreamFn()
		return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
			if isAnthropicModel(opts.Model) {
				return anthropic(ctx, reqCtx, opts)
			}
			return openai(ctx, reqCtx, opts)
		}
	case "anthropic":
		base := baseURL
		if base == "" {
			base = os.Getenv("ANTHROPIC_BASE_URL")
		}
		if base == "" {
			base = defaultAnthropicBaseURL
		}
		return (&AnthropicClient{BaseURL: strings.TrimRight(base, "/"), APIKey: apiKey, Headers: headers, HTTPClient: httpClient}).StreamFn()
	default:
		base := baseURL
		if base == "" {
			base = os.Getenv("OPENAI_BASE_URL")
		}
		if base == "" {
			base = "https://api.openai.com"
		}
		return (&OpenAICompletionsClient{BaseURL: strings.TrimRight(base, "/"), APIKey: apiKey, Headers: headers, HTTPClient: httpClient}).StreamFn()
	}
}
