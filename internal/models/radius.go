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

const (
	radiusCatalogWait    = 10 * time.Second
	defaultRadiusGateway = "https://radius.pi.dev"
)

var radiusCatalogRetry = 500 * time.Millisecond

// RadiusGateway is RADIUS_GATEWAY or PIGO_RADIUS_GATEWAY, else the public host.
func RadiusGateway() string {
	g := strings.TrimRight(strings.TrimSpace(os.Getenv("RADIUS_GATEWAY")), "/")
	if g == "" {
		g = strings.TrimRight(strings.TrimSpace(os.Getenv("PIGO_RADIUS_GATEWAY")), "/")
	}
	if g == "" {
		g = defaultRadiusGateway
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
	return refreshRadiusCatalog(context.Background(), store, os.Getenv("RADIUS_API_KEY"))
}

func refreshRadiusCatalog(ctx context.Context, store CatalogStore, token string) error {
	gateway := RadiusGateway()
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

// WaitForRadiusCatalog polls the Radius gateway until the overlay is non-empty
// or ctx ends. Timeout is not an error: the caller should keep the credential.
func WaitForRadiusCatalog(ctx context.Context, store CatalogStore, token string, notify func(string)) bool {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, radiusCatalogWait)
		defer cancel()
	}
	notifyRadiusCatalog(notify, "Waiting for Radius model catalog…")
	for {
		err := refreshRadiusCatalog(ctx, store, token)
		if err == nil && len(remoteOverlay("radius")) > 0 {
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

// RadiusOverlayReady reports whether the gateway catalog overlay is present.
func RadiusOverlayReady() bool {
	return len(remoteOverlay("radius")) > 0
}
