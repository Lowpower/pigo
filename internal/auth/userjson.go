package auth

import (
	"sync"

	"github.com/Lowpower/pigo/internal/models"
)

var (
	userJSONAuthMu  sync.Mutex
	userJSONAuthIDs = map[string]bool{}
)

// RegisterUserJSON adds API-key auth for unknown models.json provider ids.
// Builtin auth entries are left unchanged. models.json apiKey is used when
// auth.json has no credential for that id.
func RegisterUserJSON() {
	userJSONAuthMu.Lock()
	defer userJSONAuthMu.Unlock()
	for _, p := range models.UserJSONProviders() {
		if _, found := Lookup(p.ID); found && !userJSONAuthIDs[p.ID] {
			continue
		}
		id := p.ID
		key := p.APIKey
		registerProvider(Provider{
			ID: id,
			APIKey: &APIKeyHandler{
				Name:  id + " API key",
				Login: promptAPIKey(id + " API key"),
				Resolve: func() *Result {
					if key == "" {
						return nil
					}
					return &Result{Auth: ModelAuth{APIKey: key}, Source: "models.json"}
				},
			},
		})
		userJSONAuthIDs[id] = true
	}
}
