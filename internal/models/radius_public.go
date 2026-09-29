package models

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// radiusPublicJSON is the public Radius catalog.
// Snapshot: @earendil-works/pi-ai 0.87.1 dist/providers/data/radius.json
// (pi-messages entries).
//
//go:embed radius_public.json
var radiusPublicJSON []byte

type radiusPublicFile struct {
	PiMessages map[string]radiusPublicModel `json:"pi-messages"`
}

type radiusPublicModel struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Reasoning        bool               `json:"reasoning"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
	Input            []string           `json:"input"`
	Cost             *Cost              `json:"cost"`
	ContextWindow    int                `json:"contextWindow"`
	MaxTokens        int                `json:"maxTokens"`
	BaseURL          string             `json:"baseUrl"`
}

var (
	radiusPublicOnce sync.Once
	radiusPublicList []Model
	radiusPublicErr  error
)

func publicRadiusModels() []Model {
	radiusPublicOnce.Do(loadPublicRadius)
	if radiusPublicErr != nil {
		return nil
	}
	out := make([]Model, len(radiusPublicList))
	copy(out, radiusPublicList)
	return out
}

func loadPublicRadius() {
	var file radiusPublicFile
	if err := json.Unmarshal(radiusPublicJSON, &file); err != nil {
		radiusPublicErr = fmt.Errorf("parse radius public catalog: %w", err)
		return
	}
	ids := make([]string, 0, len(file.PiMessages))
	for id := range file.PiMessages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		src := file.PiMessages[id]
		if src.ID == "" {
			src.ID = id
		}
		reasoning := src.Reasoning
		radiusPublicList = append(radiusPublicList, Model{
			Provider:         "radius",
			ID:               src.ID,
			Name:             src.Name,
			API:              "pigo-messages",
			BaseURL:          src.BaseURL,
			Cost:             src.Cost,
			MaxTokens:        src.MaxTokens,
			ContextWindow:    src.ContextWindow,
			Reasoning:        &reasoning,
			Input:            src.Input,
			ThinkingLevelMap: src.ThinkingLevelMap,
		})
	}
	if len(radiusPublicList) == 0 {
		radiusPublicErr = fmt.Errorf("radius public catalog is empty")
	}
}
