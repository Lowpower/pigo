package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	openRouterUserStoreID = "openrouter-user"
	openRouterUserTimeout = 15 * time.Second
	defaultOpenRouterBase = "https://openrouter.ai/api/v1"
)

// refreshOpenRouterUser narrows the openrouter chat catalog to the models the
// current key may use. No key leaves the public snapshot. Offline reuses the
// last saved list. 401, 403, 404, and other failures leave both the snapshot
// and the saved list alone.
func refreshOpenRouterUser(agentDir string, store CatalogStore, offline bool) {
	setOpenRouterUserCatalog(nil, false)
	key := OpenRouterKey(agentDir)
	if key == "" {
		return
	}
	if offline {
		applyStoredOpenRouterUser(store)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), openRouterUserTimeout)
	defer cancel()
	entries, status, err := fetchOpenRouterUserModels(ctx, openRouterBaseURL(), key)
	if err != nil || status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound {
		return
	}
	personalized := personalizeOpenRouter(openRouterModels(), entries)
	setOpenRouterUserCatalog(personalized, true)
	if store != nil {
		_ = store.Write(openRouterUserStoreID, StoreEntry{
			Models:    personalized,
			CheckedAt: time.Now().UnixMilli(),
		})
	}
}

func applyStoredOpenRouterUser(store CatalogStore) {
	if store == nil {
		return
	}
	entry, ok, err := store.Read(openRouterUserStoreID)
	if err != nil || !ok {
		return
	}
	setOpenRouterUserCatalog(entry.Models, true)
}

func openRouterBaseURL() string {
	spec, ok := LookupProvider("openrouter")
	if !ok || strings.TrimSpace(spec.BaseURL) == "" {
		return defaultOpenRouterBase
	}
	return spec.BaseURL
}

func openRouterUserURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = defaultOpenRouterBase
	}
	return base + "/models/user"
}

func openRouterModels() []Model {
	var out []Model
	for _, m := range AllModels() {
		if m.Provider == "openrouter" {
			out = append(out, m)
		}
	}
	return out
}

type openRouterAPIModel struct {
	ID            string                 `json:"id"`
	ContextLength *int                   `json:"context_length"`
	TopProvider   *openRouterTopProvider `json:"top_provider"`
	Pricing       *openRouterAPIPricing  `json:"pricing"`
}

type openRouterTopProvider struct {
	ContextLength       *int `json:"context_length"`
	MaxCompletionTokens *int `json:"max_completion_tokens"`
}

type openRouterAPIPricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
}

func fetchOpenRouterUserModels(ctx context.Context, base, key string) ([]openRouterAPIModel, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterUserURL(base), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("authorization", "Bearer "+key)
	req.Header.Set("user-agent", "pigo")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("openrouter model list: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	var probe struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || len(probe.Data) == 0 || probe.Data[0] != '[' {
		return nil, resp.StatusCode, fmt.Errorf("invalid openrouter model list response")
	}
	var entries []openRouterAPIModel
	if err := json.Unmarshal(probe.Data, &entries); err != nil {
		return nil, resp.StatusCode, err
	}
	return entries, resp.StatusCode, nil
}

// personalizeOpenRouter keeps chat models present in both catalog and entries.
// Image and classifier rows stay. A missing or zero limit keeps the catalog
// value. Prices are dollars per million tokens; missing, invalid, and negative
// per-token prices are zero.
func personalizeOpenRouter(catalog []Model, entries []openRouterAPIModel) []Model {
	allowed := make(map[string]openRouterAPIModel, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			continue
		}
		allowed[entry.ID] = entry
	}
	out := make([]Model, 0, len(catalog))
	for _, m := range catalog {
		if m.ModelType() != "chat" {
			out = append(out, m)
			continue
		}
		entry, ok := allowed[m.ID]
		if !ok {
			continue
		}
		out = append(out, applyOpenRouterUserFields(m, entry))
	}
	return out
}

func applyOpenRouterUserFields(m Model, entry openRouterAPIModel) Model {
	if entry.TopProvider != nil {
		if n, ok := positiveInt(entry.TopProvider.ContextLength); ok {
			m.ContextWindow = n
		} else if n, ok := positiveInt(entry.ContextLength); ok {
			m.ContextWindow = n
		}
		if n, ok := positiveInt(entry.TopProvider.MaxCompletionTokens); ok {
			m.MaxTokens = n
		}
	} else if n, ok := positiveInt(entry.ContextLength); ok {
		m.ContextWindow = n
	}
	if entry.Pricing != nil {
		m.Cost = &Cost{
			Input:      openRouterPerMillion(entry.Pricing.Prompt),
			Output:     openRouterPerMillion(entry.Pricing.Completion),
			CacheRead:  openRouterPerMillion(entry.Pricing.InputCacheRead),
			CacheWrite: openRouterPerMillion(entry.Pricing.InputCacheWrite),
		}
	}
	return m
}

func positiveInt(v *int) (int, bool) {
	if v == nil || *v <= 0 {
		return 0, false
	}
	return *v, true
}

// openRouterPerMillion converts an OpenRouter $/token string to $/1M tokens.
// Missing, invalid, and negative values are 0.
func openRouterPerMillion(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	perToken, err := strconv.ParseFloat(raw, 64)
	if err != nil || perToken < 0 || math.IsNaN(perToken) || math.IsInf(perToken, 0) {
		return 0
	}
	perMillion := perToken * 1_000_000
	return math.Round(perMillion*1_000_000) / 1_000_000
}
