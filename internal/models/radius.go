package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const radiusCatalogWait = 10 * time.Second

var radiusCatalogRetry = 500 * time.Millisecond

// RadiusGateway is RADIUS_GATEWAY or PIGO_RADIUS_GATEWAY (no built-in host).
func RadiusGateway() string {
	g := strings.TrimRight(strings.TrimSpace(os.Getenv("RADIUS_GATEWAY")), "/")
	if g == "" {
		g = strings.TrimRight(strings.TrimSpace(os.Getenv("PIGO_RADIUS_GATEWAY")), "/")
	}
	return g
}

type radiusGatewayConfig struct {
	BaseURL string               `json:"baseUrl"`
	Models  []radiusGatewayModel `json:"models"`
}

type radiusGatewayModel struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Reasoning        *bool              `json:"reasoning"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
	Input            []string           `json:"input"`
	Cost             *Cost              `json:"cost"`
	ContextWindow    int                `json:"contextWindow"`
	MaxTokens        int                `json:"maxTokens"`
}

func refreshRadius(store CatalogStore) error {
	return refreshRadiusProvider(context.Background(), store, "radius", RadiusGateway(), os.Getenv("RADIUS_API_KEY"), true)
}

func refreshRadiusProvider(ctx context.Context, store CatalogStore, providerID, gateway, token string, seedPublic bool) error {
	gateway = strings.TrimRight(strings.TrimSpace(gateway), "/")
	if gateway == "" {
		if seedPublic {
			seedPublicRadius(store)
		}
		return nil
	}
	if token == "" {
		token = os.Getenv("RADIUS_API_KEY")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway+"/v1/config", nil)
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/json")
	if token != "" {
		req.Header.Set("authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: remoteAttemptTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("could not load Radius config from %s: %d", gateway, resp.StatusCode)
	}
	var cfg radiusGatewayConfig
	if err := json.Unmarshal(body, &cfg); err != nil || cfg.BaseURL == "" {
		return fmt.Errorf("invalid Radius config from %s", gateway)
	}
	out := modelsFromRadiusConfig(providerID, cfg)
	SetRemoteOverlay(providerID, out)
	if store != nil {
		_ = store.Write(providerID, StoreEntry{Models: out, CheckedAt: time.Now().UnixMilli()})
	}
	return nil
}

func modelsFromRadiusConfig(providerID string, cfg radiusGatewayConfig) []Model {
	base := strings.TrimRight(cfg.BaseURL, "/")
	out := make([]Model, 0, len(cfg.Models))
	for _, m := range cfg.Models {
		if m.ID == "" {
			continue
		}
		out = append(out, Model{
			Provider:         providerID,
			ID:               m.ID,
			Name:             m.Name,
			API:              "pigo-messages",
			BaseURL:          base,
			Cost:             m.Cost,
			MaxTokens:        m.MaxTokens,
			ContextWindow:    m.ContextWindow,
			Reasoning:        m.Reasoning,
			Input:            m.Input,
			ThinkingLevelMap: m.ThinkingLevelMap,
		})
	}
	return out
}

// seedPublicRadius writes the built-in public list when models-store has no
// radius entry. An existing gateway cache is left in place.
func seedPublicRadius(store CatalogStore) {
	if store == nil {
		return
	}
	spec, ok := LookupProvider("radius")
	if !ok || spec.RadiusGateway != "" {
		return
	}
	if _, found, err := store.Read("radius"); err != nil || found {
		return
	}
	models := publicRadiusModels()
	if len(models) == 0 {
		return
	}
	_ = store.Write("radius", StoreEntry{Models: models, CheckedAt: time.Now().UnixMilli()})
}

// registerRadiusGatewayProvider installs a models.json oauth:radius gateway
// with an empty baseline. It does not inherit the public catalog.
func registerRadiusGatewayProvider(id, name, gateway, api string) {
	gateway = strings.TrimRight(strings.TrimSpace(gateway), "/")
	if api == "" {
		api = "pigo-messages"
	}
	if name == "" {
		name = id
	}
	providerID := id
	captured := gateway
	RegisterProvider(ProviderSpec{
		ID:            providerID,
		Name:          name,
		BaseURL:       captured,
		DefaultAPI:    api,
		RadiusGateway: captured,
		RefreshModels: func(store CatalogStore) error {
			return refreshRadiusProvider(context.Background(), store, providerID, captured, os.Getenv("RADIUS_API_KEY"), false)
		},
	})
}

// WaitForRadiusCatalog polls the built-in Radius gateway until its overlay is
// non-empty or ctx ends. Timeout is not an error: the caller should keep the credential.
func WaitForRadiusCatalog(ctx context.Context, store CatalogStore, token string, notify func(string)) bool {
	return WaitForRadiusProvider(ctx, store, "radius", RadiusGateway(), token, notify)
}

// WaitForRadiusProvider polls gateway until providerID's overlay is non-empty
// or ctx ends. With no gateway, the built-in public catalog counts as ready.
func WaitForRadiusProvider(ctx context.Context, store CatalogStore, providerID, gateway, token string, notify func(string)) bool {
	gateway = strings.TrimRight(strings.TrimSpace(gateway), "/")
	if gateway == "" {
		if providerID == "radius" && len(publicRadiusModels()) > 0 {
			return true
		}
		notifyRadiusCatalog(notify, "Radius model catalog timed out; models may be unavailable until refresh.")
		return false
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, radiusCatalogWait)
		defer cancel()
	}
	notifyRadiusCatalog(notify, "Waiting for Radius model catalog…")
	for {
		err := refreshRadiusProvider(ctx, store, providerID, gateway, token, false)
		if err == nil && len(remoteOverlay(providerID)) > 0 {
			return true
		}
		timer := time.NewTimer(radiusCatalogRetry)
		select {
		case <-ctx.Done():
			timer.Stop()
			notifyRadiusCatalog(notify, "Radius model catalog timed out; models may be unavailable until refresh.")
			return false
		case <-timer.C:
		}
	}
}

func notifyRadiusCatalog(notify func(string), msg string) {
	if notify != nil {
		notify(msg)
	}
}
