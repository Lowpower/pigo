package models

import (
	"strings"
	"sync"
)

// StoreEntry is one provider's persisted dynamic catalog.
type StoreEntry struct {
	Models       []Model `json:"models"`
	ETag         string  `json:"etag,omitempty"`
	LastModified int64   `json:"lastModified,omitempty"`
	CheckedAt    int64   `json:"checkedAt,omitempty"`
}

// CatalogStore persists per-provider catalog overlays.
type CatalogStore interface {
	Read(providerID string) (StoreEntry, bool, error)
	Write(providerID string, entry StoreEntry) error
}

// UserJSONProvider is one models.json provider overlay (including optional apiKey).
type UserJSONProvider struct {
	ID      string
	Name    string
	BaseURL string
	API     string
	APIKey  string
	OAuth   string
	// Headers are models.json provider headers. They are not copied onto
	// ProviderSpec.Headers, which stays the builtin default layer.
	Headers map[string]string
}

var (
	overlayMu     sync.Mutex
	remote        = map[string][]Model{}
	userJSON      = map[string][]Model{}
	userJSONSpecs = map[string]UserJSONProvider{}
	// openRouterUserActive replaces openrouter chat models with openRouterUserModels.
	// Inactive leaves the public snapshot in place, even if a list is on disk.
	openRouterUserActive bool
	openRouterUserModels []Model
	openRouterKey        func(agentDir string) string
)

// UserJSONProviderByID returns one provider last loaded from models.json.
func UserJSONProviderByID(id string) (UserJSONProvider, bool) {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	p, ok := userJSONSpecs[id]
	return p, ok
}

// UserJSONProviders returns providers last loaded from models.json.
func UserJSONProviders() []UserJSONProvider {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	out := make([]UserJSONProvider, 0, len(userJSONSpecs))
	for _, p := range userJSONSpecs {
		out = append(out, p)
	}
	return out
}

func setUserJSONProviders(list []UserJSONProvider) {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	userJSONSpecs = map[string]UserJSONProvider{}
	for _, p := range list {
		if p.ID == "" {
			continue
		}
		userJSONSpecs[p.ID] = p
	}
}

func remoteOverlay(providerID string) []Model {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	return append([]Model(nil), remote[providerID]...)
}

func userOverlay(providerID string) []Model {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	return append([]Model(nil), userJSON[providerID]...)
}

// SetRemoteOverlay replaces the remote catalog overlay for a provider.
func SetRemoteOverlay(providerID string, models []Model) {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	if len(models) == 0 {
		delete(remote, providerID)
		return
	}
	remote[providerID] = append([]Model(nil), models...)
}

// SetUserOverlay replaces the models.json overlay for a provider.
func SetUserOverlay(providerID string, models []Model) {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	if len(models) == 0 {
		delete(userJSON, providerID)
		return
	}
	userJSON[providerID] = append([]Model(nil), models...)
}

// SetOpenRouterKey registers the bearer token used for GET /models/user.
// auth installs the same lookup the request path uses. Nil means no key.
func SetOpenRouterKey(fn func(agentDir string) string) {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	openRouterKey = fn
}

// OpenRouterKey returns the registered OpenRouter bearer token, or "".
func OpenRouterKey(agentDir string) string {
	overlayMu.Lock()
	fn := openRouterKey
	overlayMu.Unlock()
	if fn == nil {
		return ""
	}
	return strings.TrimSpace(fn(agentDir))
}

func setOpenRouterUserCatalog(models []Model, active bool) {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	openRouterUserActive = active
	if !active {
		openRouterUserModels = nil
		return
	}
	openRouterUserModels = append([]Model(nil), models...)
}

func openRouterUserCatalog() ([]Model, bool) {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	if !openRouterUserActive {
		return nil, false
	}
	return append([]Model(nil), openRouterUserModels...), true
}

// ClearOverlays drops remote and user overlays (tests).
func ClearOverlays() {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	remote = map[string][]Model{}
	userJSON = map[string][]Model{}
	userJSONSpecs = map[string]UserJSONProvider{}
	openRouterUserActive = false
	openRouterUserModels = nil
}
