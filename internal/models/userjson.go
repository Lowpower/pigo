package models

import (
	"encoding/json"
	"os"
)

type userModelsFile struct {
	Providers map[string]userProvider `json:"providers"`
}

type userProvider struct {
	BaseURL string  `json:"baseUrl"`
	API     string  `json:"api"`
	Models  []Model `json:"models"`
}

// LoadUserJSON applies ~/.pigo/agent/models.json overlays (and registers unknown providers).
// A missing file is not an error.
func LoadUserJSON(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file userModelsFile
	if err := json.Unmarshal(b, &file); err != nil {
		return err
	}
	for id, p := range file.Providers {
		models := make([]Model, 0, len(p.Models))
		for _, m := range p.Models {
			m.Provider = id
			if m.API == "" {
				m.API = p.API
			}
			if m.BaseURL == "" {
				m.BaseURL = p.BaseURL
			}
			models = append(models, m)
		}
		spec, ok := LookupProvider(id)
		if !ok {
			defID := ""
			if len(models) > 0 {
				defID = models[0].ID
			}
			RegisterProvider(ProviderSpec{
				ID:         id,
				BaseURL:    p.BaseURL,
				DefaultAPI: p.API,
				DefaultID:  defID,
			})
		} else {
			if p.API != "" {
				spec.DefaultAPI = p.API
			}
			if p.BaseURL != "" {
				spec.BaseURL = p.BaseURL
			}
			RegisterProvider(spec)
		}
		SetUserOverlay(id, models)
	}
	return nil
}
