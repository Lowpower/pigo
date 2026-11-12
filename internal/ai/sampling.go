package ai

import (
	"strings"

	"github.com/Lowpower/pigo/internal/models"
	"github.com/openai/openai-go/responses"
)

// ResolveSamplingParams merges the catalog model's samplingParams, the
// samplingParamsByThinkingLevel entry for the clamped thinking level, and
// per-request overrides. Caller keys win. An empty result is nil.
// Models whose API is not OpenAI-compatible contribute no defaults.
// An empty thinking level is off. A missing or null level entry does not
// replace samplingParams; keys the entry omits stay.
func ResolveSamplingParams(provider, modelID, thinking string, caller map[string]any) map[string]any {
	var base map[string]any
	if provider != "" && modelID != "" {
		if m, ok := models.Lookup(provider, modelID); ok && samplingParamsAPI(m.API) {
			base = m.SamplingParams
			if entry, ok := samplingParamsForThinking(m, thinking); ok {
				base = models.MergeSamplingParams(base, entry)
			}
		}
	}
	return models.MergeSamplingParams(base, caller)
}

// samplingParamsForThinking is the level entry selected the way pi clamps a
// thinking level. off that the model marks unsupported moves to the next
// supported level. The wire effort still uses RequestThinkingLevel.
func samplingParamsForThinking(m models.Model, thinking string) (map[string]any, bool) {
	if len(m.SamplingParamsByThinkingLevel) == 0 {
		return nil, false
	}
	entry, ok := m.SamplingParamsByThinkingLevel[models.ClampThinking(thinking, m)]
	if !ok || len(entry) == 0 {
		return nil, false
	}
	return entry, true
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
func applyResponsesSampling(params *responses.ResponseNewParams, extra map[string]any, reqCtx Context, opts Options, keepReasoning bool) {
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
	if keepReasoning || reasoningEffort(opts) != "" {
		skip["reasoning"] = true
		skip["include"] = true
	}
	if len(reqCtx.Tools) > 0 {
		skip["tools"] = true
	}
	for k := range extra {
		skip[k] = true
	}
	for k, v := range ResolveSamplingParams(opts.Provider, opts.Model, opts.Thinking, opts.SamplingParams) {
		if skip[k] {
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		params.SetExtraFields(extra)
	}
}
