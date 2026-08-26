package models

import (
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

var (
	overlayMu sync.Mutex
	remote    = map[string][]Model{}
	userJSON  = map[string][]Model{}
)

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

// ClearOverlays drops remote and user overlays (tests).
func ClearOverlays() {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	remote = map[string][]Model{}
	userJSON = map[string][]Model{}
}
