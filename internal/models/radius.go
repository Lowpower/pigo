package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// RadiusGateway is RADIUS_GATEWAY or PIGO_RADIUS_GATEWAY (no built-in host).
func RadiusGateway() string {
	g := strings.TrimRight(strings.TrimSpace(os.Getenv("RADIUS_GATEWAY")), "/")
	if g == "" {
		g = strings.TrimRight(strings.TrimSpace(os.Getenv("PIGO_RADIUS_GATEWAY")), "/")
	}
	return g
}

type radiusGatewayConfig struct {
	BaseURL string `json:"baseUrl"`
	Models  []struct {
		ID string `json:"id"`
	} `json:"models"`
}

func refreshRadius(store CatalogStore) error {
	gateway := RadiusGateway()
	key := os.Getenv("RADIUS_API_KEY")
	if gateway == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, gateway+"/v1/config", nil)
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/json")
	if key != "" {
		req.Header.Set("authorization", "Bearer "+key)
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
	out := make([]Model, 0, len(cfg.Models))
	for _, m := range cfg.Models {
		if m.ID == "" {
			continue
		}
		out = append(out, Model{
			Provider: "radius",
			ID:       m.ID,
			API:      "pigo-messages",
			BaseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		})
	}
	SetRemoteOverlay("radius", out)
	if store != nil {
		_ = store.Write("radius", StoreEntry{Models: out, CheckedAt: time.Now().UnixMilli()})
	}
	return nil
}
