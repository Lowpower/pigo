package ai

import (
	"strings"

	"github.com/Lowpower/pigo/internal/models"
	"github.com/openai/openai-go/responses"
)

// ResolveSamplingParams merges the catalog model's samplingParams with
// per-request overrides. Caller keys win. An empty result is nil.
// Models whose API is not OpenAI-compatible contribute no defaults.
func ResolveSamplingParams(provider, modelID string, caller map[string]any) map[string]any {
	var base map[string]any
	if provider != "" && modelID != "" {
		if m, ok := models.Lookup(provider, modelID); ok && samplingParamsAPI(m.API) {
			base = m.SamplingParams
		}
	}
	return models.MergeSamplingParams(base, caller)
}

func samplingParamsAPI(api string) bool {
	switch strings.TrimSpace(api) {
	case "", "openai-completions", "openai-responses", "azure-openai-responses":
		return true
	default:
		return false
	}
}

// applySamplingParams copies params into dst for keys dst does not already have.
func applySamplingParams(dst map[string]any, params map[string]any) {
	if len(params) == 0 || dst == nil {
		return
	}
	for k, v := range params {
		if _, exists := dst[k]; exists {
			continue
		}
		dst[k] = v
	}
}

// applyResponsesSampling merges sampling fields into extra, then attaches them
// to the typed Responses request. Keys already written by the client stay put.
func applyResponsesSampling(params *responses.ResponseNewParams, extra map[string]any, reqCtx Context, opts Options) {
	if params == nil {
		return
	}
	if extra == nil {
		extra = map[string]any{}
	}
	skip := map[string]bool{
		"model":  true,
		"input":  true,
		"store":  true,
		"stream": true,
	}
	if reqCtx.System != "" {
		skip["instructions"] = true
	}
	if opts.MaxTokens > 0 && supportsMaxOutputTokens(opts) {
		skip["max_output_tokens"] = true
	}
	if clampPromptCacheKey(opts.SessionID) != "" {
		skip["prompt_cache_key"] = true
	}
	if reasoningEffort(opts) != "" {
		skip["reasoning"] = true
		skip["include"] = true
	}
	if len(reqCtx.Tools) > 0 {
		skip["tools"] = true
	}
	for k := range extra {
		skip[k] = true
	}
	for k, v := range ResolveSamplingParams(opts.Provider, opts.Model, opts.SamplingParams) {
		if skip[k] {
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		params.SetExtraFields(extra)
	}
}
