package models

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// catalogJSON is the offline model catalog.
// Snapshot of https://models.dev/api.json.
// Upstream data is MIT licensed, Copyright (c) 2025 models.dev.
// Regenerate with: go run ./internal/models/cataloggen
//
//go:embed catalog.json
var catalogJSON []byte

// catalogFile is the embedded snapshot. sha256 is the models.dev api.json body.
type catalogFile struct {
	License   string             `json:"license"`
	Copyright string             `json:"copyright"`
	Source    string             `json:"source"`
	SHA256    string             `json:"sha256"`
	Providers map[string][]Model `json:"providers"`
}

func loadCatalogFile() (catalogFile, error) {
	var file catalogFile
	if err := json.Unmarshal(catalogJSON, &file); err != nil {
		return catalogFile{}, fmt.Errorf("parse embedded catalog: %w", err)
	}
	return file, nil
}

func applyEmbeddedCatalog() error {
	file, err := loadCatalogFile()
	if err != nil {
		return err
	}
	for id, list := range file.Providers {
		spec, ok := LookupProvider(id)
		if !ok || len(list) == 0 {
			continue
		}
		models := make([]Model, len(list))
		copy(models, list)
		for i := range models {
			models[i].Provider = id
			if models[i].API == "" {
				models[i].API = spec.DefaultAPI
			}
			models[i] = annotateBuiltin(models[i])
		}
		spec.Models = models
		RegisterProvider(spec)
	}
	return nil
}

// annotateBuiltin keeps pigo policy that models.dev does not publish.
func annotateBuiltin(m Model) Model {
	if m.Provider == "anthropic" && m.PromptCache == nil {
		m.PromptCache = anthropicPromptCache()
	}
	if m.Provider == "openai" && m.ID == "gpt-6-astra" && m.Compat == nil {
		m.Compat = &Compat{SupportsExplicitPromptCacheMode: true}
	}
	return m
}

// CatalogDefaultOK reports whether providerID's default model is in list
// with a non-zero context window and max tokens.
func CatalogDefaultOK(providerID, defaultID string, list []Model) error {
	for _, m := range list {
		if m.ID != defaultID {
			continue
		}
		if m.ContextWindow <= 0 || m.MaxTokens <= 0 {
			return fmt.Errorf("%s/%s contextWindow=%d maxTokens=%d", providerID, defaultID, m.ContextWindow, m.MaxTokens)
		}
		return nil
	}
	return fmt.Errorf("%s default %q is not in the catalog", providerID, defaultID)
}
