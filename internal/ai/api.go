package ai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/models"
)

// ClientConfig is the resolved auth/transport for one API factory call.
type ClientConfig struct {
	APIKey     string
	BaseURL    string
	Headers    map[string]string
	HTTPClient *http.Client
}

// APIFactory builds a StreamFn from resolved client config.
type APIFactory func(ClientConfig) StreamFn

var (
	apiMu       sync.Mutex
	apiByID     = map[string]APIFactory{}
	idleTimeout = 5 * time.Minute
)

func init() {
	registerBuiltinAPIs()
}

func registerBuiltinAPIs() {
	RegisterAPI("anthropic-messages", func(cfg ClientConfig) StreamFn {
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" {
			base = defaultAnthropicBaseURL
		}
		return (&AnthropicClient{BaseURL: base, APIKey: cfg.APIKey, Headers: cfg.Headers, HTTPClient: httpClient(cfg)}).StreamFn()
	})
	RegisterAPI("openai-completions", func(cfg ClientConfig) StreamFn {
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" {
			base = "https://api.openai.com"
		}
		return (&OpenAICompletionsClient{BaseURL: base, APIKey: cfg.APIKey, Headers: cfg.Headers, HTTPClient: httpClient(cfg)}).StreamFn()
	})
	RegisterAPI("openai-responses", func(cfg ClientConfig) StreamFn {
		return (&OpenAIResponsesClient{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Headers: cfg.Headers, HTTPClient: httpClient(cfg)}).StreamFn()
	})
	RegisterAPI("google-generative-ai", func(cfg ClientConfig) StreamFn {
		return (&GoogleClient{APIKey: cfg.APIKey, Headers: cfg.Headers, HTTPClient: httpClient(cfg)}).StreamFn()
	})
	RegisterAPI("bedrock-converse-stream", func(cfg ClientConfig) StreamFn {
		return (&BedrockClient{APIKey: cfg.APIKey, Headers: cfg.Headers, HTTPClient: httpClient(cfg)}).StreamFn()
	})
	RegisterAPI("opencode", func(cfg ClientConfig) StreamFn {
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" {
			base = os.Getenv("OPENCODE_BASE_URL")
		}
		if base == "" {
			base = defaultOpenCodeBaseURL
		}
		base = strings.TrimRight(base, "/")
		hc := httpClient(cfg)
		anthropic := (&AnthropicClient{BaseURL: base, APIKey: cfg.APIKey, Headers: cfg.Headers, HTTPClient: hc}).StreamFn()
		openai := (&OpenAICompletionsClient{BaseURL: base, APIKey: cfg.APIKey, Headers: cfg.Headers, HTTPClient: hc}).StreamFn()
		return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
			if isAnthropicModel(opts.Model) {
				return anthropic(ctx, reqCtx, opts)
			}
			return openai(ctx, reqCtx, opts)
		}
	})
}

// RegisterAPI adds or replaces an API stream factory.
func RegisterAPI(id string, f APIFactory) {
	apiMu.Lock()
	defer apiMu.Unlock()
	apiByID[id] = f
}

// LookupAPI returns a registered API factory.
func LookupAPI(id string) (APIFactory, bool) {
	apiMu.Lock()
	defer apiMu.Unlock()
	f, ok := apiByID[id]
	return f, ok
}

// SetHTTPIdleTimeout sets the default HTTP client timeout used when none is provided.
func SetHTTPIdleTimeout(d time.Duration) {
	apiMu.Lock()
	defer apiMu.Unlock()
	idleTimeout = d
}

func httpClient(cfg ClientConfig) *http.Client {
	if cfg.HTTPClient != nil {
		return cfg.HTTPClient
	}
	apiMu.Lock()
	d := idleTimeout
	apiMu.Unlock()
	if d == 0 {
		return &http.Client{}
	}
	return &http.Client{Timeout: d}
}

// StreamFor dispatches by models.APIFor(provider, opts.Model).
func StreamFor(provider string, cfg ClientConfig) StreamFn {
	return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
		opts.ThinkingBudget = models.BudgetTokens(opts.Thinking)
		c := cfg
		if m, ok := models.Lookup(provider, opts.Model); ok && m.BaseURL != "" && c.BaseURL == "" {
			c.BaseURL = m.BaseURL
		}
		if c.BaseURL == "" {
			if spec, ok := models.LookupProvider(provider); ok {
				c.BaseURL = spec.BaseURL
			}
		}
		if spec, ok := models.LookupProvider(provider); ok && len(spec.Headers) > 0 {
			merged := make(map[string]string, len(spec.Headers)+len(c.Headers))
			for k, v := range spec.Headers {
				merged[k] = v
			}
			for k, v := range c.Headers {
				merged[k] = v
			}
			c.Headers = merged
		}
		api := models.APIFor(provider, opts.Model)
		if api == "" {
			api = "unknown"
		}
		f, ok := LookupAPI(api)
		if !ok {
			return errorStreamProvider(opts.Model, api, fmt.Sprintf("unknown api %q", api)), nil
		}
		return f(c)(ctx, reqCtx, opts)
	}
}

// StreamWithAuth builds a StreamFn from resolved provider auth.
func StreamWithAuth(provider, apiKey, baseURL string, headers map[string]string) StreamFn {
	return StreamFor(provider, ClientConfig{APIKey: apiKey, BaseURL: baseURL, Headers: headers})
}

func envKey(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

func bedrockAmbient() bool {
	if os.Getenv("AWS_BEARER_TOKEN_BEDROCK") != "" || os.Getenv("AWS_PROFILE") != "" {
		return true
	}
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}
	if os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" || os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI") != "" {
		return true
	}
	return os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != ""
}
