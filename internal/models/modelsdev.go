package models

import (
	"encoding/json"
	"sort"
	"strings"
)

// modelsDevAliases maps a pigo provider id to the models.dev provider id
// when the two catalogs use different names.
var modelsDevAliases = map[string]string{
	"together":               "togetherai",
	"fireworks":              "fireworks-ai",
	"vercel-ai-gateway":      "vercel",
	"azure-openai-responses": "azure",
	"qwen-token-plan":        "alibaba-token-plan",
	"qwen-token-plan-cn":     "alibaba-token-plan-cn",
	"zai-coding-cn":          "zai-coding-plan",
}

// modelsDevLocalOnly are registered providers that are not sourced from models.dev.
var modelsDevLocalOnly = map[string]bool{
	"ant-ling":                   true,
	"kimi-coding":                true,
	"qwen-token-plan-individual": true,
	"openai-codex":               true,
	"llama.cpp":                  true,
	"radius":                     true,
}

// DevProviderID reports the models.dev provider id for a pigo provider.
// The second result is false when that provider keeps a local catalog.
func DevProviderID(pigoID string) (string, bool) {
	if modelsDevLocalOnly[pigoID] {
		return "", false
	}
	if alias, ok := modelsDevAliases[pigoID]; ok {
		return alias, true
	}
	return pigoID, true
}

type modelsDevModel struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	Reasoning        bool                    `json:"reasoning"`
	ReasoningOptions []modelsDevReasonOption `json:"reasoning_options"`
	Limit            modelsDevLimit          `json:"limit"`
	Cost             *modelsDevCost          `json:"cost"`
	Modalities       modelsDevModalities     `json:"modalities"`
	Family           string                  `json:"family"`
}

type modelsDevReasonOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type modelsDevCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type modelsDevModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// FromDev converts one models.dev provider object (id → model) into catalog rows.
// Image-generation and classifier models are omitted. Effort options become
// thinkingLevelMap; toggle and budget_tokens do not invent an effort map.
func FromDev(providerID, api string, raw []byte) ([]Model, error) {
	var keyed map[string]modelsDevModel
	if err := json.Unmarshal(raw, &keyed); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(keyed))
	for id := range keyed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Model, 0, len(ids))
	for _, key := range ids {
		src := keyed[key]
		if src.ID == "" {
			src.ID = key
		}
		if skipModelsDev(src) {
			continue
		}
		reasoning := src.Reasoning
		m := Model{
			Provider:      providerID,
			ID:            src.ID,
			Name:          src.Name,
			API:           api,
			MaxTokens:     src.Limit.Output,
			ContextWindow: src.Limit.Context,
			Reasoning:     &reasoning,
			Input:         append([]string(nil), src.Modalities.Input...),
		}
		if src.Cost != nil {
			m.Cost = &Cost{
				Input:      src.Cost.Input,
				Output:     src.Cost.Output,
				CacheRead:  src.Cost.CacheRead,
				CacheWrite: src.Cost.CacheWrite,
			}
		}
		if effort := effortValues(src.ReasoningOptions); effort != nil {
			m.ThinkingLevelMap = effortThinkingLevelMap(effort)
		}
		out = append(out, m)
	}
	return out, nil
}

func skipModelsDev(src modelsDevModel) bool {
	if isClassifier(src) {
		return true
	}
	if len(src.Modalities.Output) == 0 {
		return false
	}
	for _, kind := range src.Modalities.Output {
		if strings.EqualFold(kind, "text") {
			return false
		}
	}
	return true
}

func isClassifier(src modelsDevModel) bool {
	if strings.EqualFold(src.Family, "classifier") {
		return true
	}
	return strings.Contains(strings.ToLower(src.ID), "classifier")
}

func effortValues(opts []modelsDevReasonOption) []string {
	for _, opt := range opts {
		if strings.EqualFold(opt.Type, "effort") {
			return opt.Values
		}
	}
	return nil
}

func effortThinkingLevelMap(values []string) map[string]*string {
	set := map[string]bool{}
	for _, v := range values {
		set[strings.ToLower(strings.TrimSpace(v))] = true
	}
	out := make(map[string]*string, len(ThinkingLevels))
	for _, level := range ThinkingLevels {
		wire := level
		if level == "off" {
			switch {
			case set["none"]:
				wire = "none"
			case set["off"]:
				wire = "off"
			default:
				out[level] = nil
				continue
			}
		} else if !set[level] {
			out[level] = nil
			continue
		}
		s := wire
		out[level] = &s
	}
	return out
}
