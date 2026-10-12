package models

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// openRouterReasoningJSON is reasoning metadata from
// https://openrouter.ai/api/v1/models. Regenerate with:
//
//	go run ./internal/models/cataloggen -openrouter-reasoning
//
//go:embed openrouter_reasoning.json
var openRouterReasoningJSON []byte

type openRouterReasoningFile struct {
	Source string                         `json:"source"`
	Models map[string]OpenRouterReasoning `json:"models"`
}

var (
	openRouterReasoningOnce sync.Once
	openRouterReasoningByID map[string]OpenRouterReasoning
)

func lookupOpenRouterReasoning(id string) (OpenRouterReasoning, bool) {
	openRouterReasoningOnce.Do(func() {
		var file openRouterReasoningFile
		if err := json.Unmarshal(openRouterReasoningJSON, &file); err != nil {
			panic(fmt.Errorf("parse openrouter reasoning snapshot: %w", err))
		}
		if file.Models == nil {
			file.Models = map[string]OpenRouterReasoning{}
		}
		openRouterReasoningByID = file.Models
	})
	meta, ok := openRouterReasoningByID[id]
	return meta, ok
}

// EncodeOpenRouterReasoningSnapshot keeps models whose reasoning metadata
// changes the thinking-level map. The result is stable and indented.
func EncodeOpenRouterReasoningSnapshot(apiBody []byte) ([]byte, error) {
	var api struct {
		Data []struct {
			ID        string               `json:"id"`
			Reasoning *OpenRouterReasoning `json:"reasoning"`
		} `json:"data"`
	}
	if err := json.Unmarshal(apiBody, &api); err != nil {
		return nil, fmt.Errorf("parse openrouter models: %w", err)
	}
	models := make(map[string]OpenRouterReasoning, len(api.Data))
	for _, item := range api.Data {
		if item.ID == "" || item.Reasoning == nil {
			continue
		}
		if OpenRouterThinkingLevelMap(*item.Reasoning) == nil {
			continue
		}
		models[item.ID] = *item.Reasoning
	}
	raw, err := json.MarshalIndent(openRouterReasoningFile{
		Source: openRouterModelsURL,
		Models: models,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// OpenRouterModelsURL is the public model list OpenRouter reasoning metadata comes from.
const OpenRouterModelsURL = "https://openrouter.ai/api/v1/models"

const openRouterModelsURL = OpenRouterModelsURL
