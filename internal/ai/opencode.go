package ai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// defaultOpenCodeBaseURL is the OpenCode Zen gateway base. The adapters append
// /v1/messages (Anthropic), /v1/chat/completions, /v1/responses, or Gemini paths.
const defaultOpenCodeBaseURL = "https://opencode.ai/zen"

// NewOpenCodeFromEnv builds a StreamFn for the OpenCode gateway from
// OPENCODE_API_KEY (required) and OPENCODE_BASE_URL (optional; defaults to
// OpenCode Zen; set it to the OpenCode Go base for that plan).
func NewOpenCodeFromEnv() (StreamFn, bool) {
	key := os.Getenv("OPENCODE_API_KEY")
	if key == "" {
		return nil, false
	}
	base := os.Getenv("OPENCODE_BASE_URL")
	if base == "" {
		base = defaultOpenCodeBaseURL
	}
	return openCodeMux(ClientConfig{
		APIKey:     key,
		BaseURL:    strings.TrimRight(base, "/"),
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
	}), true
}

func openCodeMux(cfg ClientConfig) StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		api := guessOpenCodeAPI(opts.Model)
		f, ok := LookupAPI(api)
		if !ok {
			return errorStreamProvider(opts.Model, api, fmt.Sprintf("unknown api %q", api)), nil
		}
		return f(cfg)(ctx, reqCtx, opts)
	}
}
