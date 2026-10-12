package models

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Lowpower/pigo/internal/bom"
)

type userModelsFile struct {
	Providers map[string]userProvider `json:"providers"`
}

type userProvider struct {
	Name    string  `json:"name"`
	BaseURL string  `json:"baseUrl"`
	API     string  `json:"api"`
	APIKey  string  `json:"apiKey"`
	OAuth   string  `json:"oauth"`
	Models  []Model `json:"models"`
}

// LoadUserJSON applies ~/.pigo/agent/models.json overlays (and registers unknown providers).
// A missing file is not an error.
func LoadUserJSON(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			setUserJSONProviders(nil)
			return nil
		}
		return err
	}
	var file userModelsFile
	if err := json.Unmarshal(bom.Strip(b), &file); err != nil {
		return err
	}
	for id, p := range file.Providers {
		if strings.TrimSpace(p.OAuth) == "radius" && strings.TrimSpace(p.BaseURL) == "" {
			return fmt.Errorf("models.json provider %q: \"baseUrl\" is required when \"oauth\" is \"radius\"", id)
		}
	}
	specs := make([]UserJSONProvider, 0, len(file.Providers))
	for id, p := range file.Providers {
		oauth := strings.TrimSpace(p.OAuth)
		specs = append(specs, UserJSONProvider{
			ID:      id,
			Name:    p.Name,
			BaseURL: p.BaseURL,
			API:     p.API,
			APIKey:  p.APIKey,
			OAuth:   oauth,
		})
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
		if oauth == "radius" {
			registerRadiusGatewayProvider(id, p.Name, p.BaseURL, p.API)
		} else if spec, ok := LookupProvider(id); !ok {
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
	setUserJSONProviders(specs)
	hasKey := false
	for _, p := range file.Providers {
		if strings.TrimSpace(p.APIKey) != "" {
			hasKey = true
			break
		}
	}
	if hasKey {
		_ = os.Chmod(path, 0o600)
	}
	return nil
}
