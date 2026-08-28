package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	defaultCatalogBaseURL = "https://pi.dev"
	remoteAttemptTimeout  = 4 * time.Second
	remoteRefreshInterval = 4 * time.Hour
)

// MemoryStore is an in-memory CatalogStore for tests.
type MemoryStore struct {
	mu   sync.Mutex
	data map[string]StoreEntry
}

// Read implements CatalogStore.
func (s *MemoryStore) Read(providerID string) (StoreEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return StoreEntry{}, false, nil
	}
	e, ok := s.data[providerID]
	return e, ok, nil
}

// Write implements CatalogStore.
func (s *MemoryStore) Write(providerID string, entry StoreEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]StoreEntry{}
	}
	s.data[providerID] = entry
	return nil
}

// FileStore persists catalog overlays as models-store.json.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// OpenFileStore returns a JSON file-backed catalog store.
func OpenFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) load() map[string]StoreEntry {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return map[string]StoreEntry{}
	}
	var m map[string]StoreEntry
	if json.Unmarshal(b, &m) != nil || m == nil {
		return map[string]StoreEntry{}
	}
	return m
}

// Read implements CatalogStore.
func (s *FileStore) Read(providerID string) (StoreEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.load()[providerID]
	return e, ok, nil
}

// Write implements CatalogStore.
func (s *FileStore) Write(providerID string, entry StoreEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.load()
	m[providerID] = entry
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, append(b, '\n'), 0o644)
}

// RestoreOverlays copies stored remote catalogs into the in-memory overlay.
func RestoreOverlays(store CatalogStore) {
	if store == nil {
		return
	}
	for _, id := range ProviderIDs() {
		e, ok, err := store.Read(id)
		if err != nil || !ok {
			continue
		}
		SetRemoteOverlay(id, e.Models)
	}
}

// PrepareCatalog loads models.json, restores the on-disk store, and optionally
// refreshes from catalogBaseURL. A missing models.json is not an error.
func PrepareCatalog(agentDir, catalogBaseURL string, offline bool) error {
	if err := LoadUserJSON(filepath.Join(agentDir, "models.json")); err != nil {
		return err
	}
	store := OpenFileStore(filepath.Join(agentDir, "models-store.json"))
	RestoreOverlays(store)
	refreshLocalProviders(store)
	if offline || catalogBaseURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteAttemptTimeout)
	defer cancel()
	RefreshAll(ctx, store, catalogBaseURL, false)
	return nil
}

// DefaultCatalogBaseURL is https://pi.dev unless PIGO_CATALOG_BASE_URL is set.
func DefaultCatalogBaseURL() string {
	if v := os.Getenv("PIGO_CATALOG_BASE_URL"); v != "" {
		return v
	}
	return defaultCatalogBaseURL
}

func refreshLocalProviders(store CatalogStore) {
	if spec, ok := LookupProvider("llama.cpp"); ok && spec.RefreshModels != nil {
		_ = spec.RefreshModels(store)
	}
}

// RefreshAll revalidates remote catalogs for every registered provider.
// It returns provider ids whose refresh failed.
func RefreshAll(ctx context.Context, store CatalogStore, baseURL string, force bool) []string {
	if store == nil || baseURL == "" {
		return nil
	}
	var (
		mu     sync.Mutex
		failed []string
		wg     sync.WaitGroup
	)
	for _, id := range ProviderIDs() {
		spec, _ := LookupProvider(id)
		if spec.RefreshModels != nil {
			if err := spec.RefreshModels(store); err != nil {
				mu.Lock()
				failed = append(failed, id)
				mu.Unlock()
			}
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := RefreshProvider(ctx, store, baseURL, id, force); err != nil {
				mu.Lock()
				failed = append(failed, id)
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	return failed
}

// RefreshProvider fetches one provider catalog (ETag/304/404/501).
func RefreshProvider(ctx context.Context, store CatalogStore, baseURL, providerID string, force bool) error {
	stored, _, _ := store.Read(providerID)
	if len(stored.Models) > 0 {
		SetRemoteOverlay(providerID, stored.Models)
	}
	if !force && stored.CheckedAt > 0 && time.Since(time.UnixMilli(stored.CheckedAt)) < remoteRefreshInterval {
		return nil
	}

	u := fmt.Sprintf("%s/api/models/providers/%s", trimSlash(baseURL), providerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "pigo")
	if stored.ETag != "" && len(stored.Models) > 0 {
		req.Header.Set("if-none-match", stored.ETag)
	}

	client := &http.Client{Timeout: remoteAttemptTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	checkedAt := time.Now().UnixMilli()

	switch resp.StatusCode {
	case http.StatusNotModified:
		stored.CheckedAt = checkedAt
		return store.Write(providerID, stored)
	case http.StatusNotFound, http.StatusNotImplemented:
		SetRemoteOverlay(providerID, nil)
		return store.Write(providerID, StoreEntry{Models: nil, CheckedAt: checkedAt, LastModified: 0})
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		stored.CheckedAt = checkedAt
		_ = store.Write(providerID, stored)
		return fmt.Errorf("model catalog request failed for %s: %d", providerID, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	models, err := parseCatalog(providerID, body)
	if err != nil {
		return err
	}
	lastModified := int64(0)
	if lm := resp.Header.Get("last-modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			lastModified = t.UnixMilli()
		}
	}
	entry := StoreEntry{
		Models:       models,
		ETag:         resp.Header.Get("etag"),
		LastModified: lastModified,
		CheckedAt:    checkedAt,
	}
	SetRemoteOverlay(providerID, models)
	return store.Write(providerID, entry)
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func parseCatalog(providerID string, raw []byte) ([]Model, error) {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 {
		return nil, fmt.Errorf("invalid model catalog for provider %q", providerID)
	}
	if trim[0] == '[' {
		var arr []Model
		if err := json.Unmarshal(trim, &arr); err != nil {
			return nil, err
		}
		return stampProvider(providerID, arr), nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trim, &probe); err != nil {
		return nil, err
	}
	if modelsRaw, ok := probe["models"]; ok {
		var arr []Model
		if err := json.Unmarshal(modelsRaw, &arr); err == nil {
			return stampProvider(providerID, arr), nil
		}
	}
	var keyed map[string]Model
	if err := json.Unmarshal(trim, &keyed); err != nil {
		return nil, fmt.Errorf("invalid model catalog for provider %q", providerID)
	}
	out := make([]Model, 0, len(keyed))
	for k, m := range keyed {
		if m.ID == "" {
			m.ID = k
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return stampProvider(providerID, out), nil
}

func stampProvider(providerID string, models []Model) []Model {
	out := make([]Model, 0, len(models))
	for _, m := range models {
		if m.ID == "" {
			continue
		}
		m.Provider = providerID
		out = append(out, m)
	}
	return out
}
