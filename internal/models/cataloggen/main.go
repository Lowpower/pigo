// Command cataloggen writes internal/models/catalog.json from models.dev.
// Run from the repository root:
//
//	go run ./internal/models/cataloggen
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Lowpower/pigo/internal/models"
)

const modelsDevURL = "https://models.dev/api.json"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	body, err := fetch(modelsDevURL)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	var api map[string]json.RawMessage
	if err := json.Unmarshal(body, &api); err != nil {
		return fmt.Errorf("parse models.dev api.json: %w", err)
	}
	providers := map[string][]models.Model{}
	for _, id := range models.ProviderIDs() {
		devID, ok := models.DevProviderID(id)
		if !ok {
			continue
		}
		raw, ok := api[devID]
		if !ok {
			return fmt.Errorf("models.dev has no provider %q (pigo %s)", devID, id)
		}
		var envelope struct {
			Models json.RawMessage `json:"models"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("parse %s: %w", devID, err)
		}
		spec, ok := models.LookupProvider(id)
		if !ok {
			return fmt.Errorf("missing provider spec %s", id)
		}
		list, err := models.FromDev(id, spec.DefaultAPI, envelope.Models)
		if err != nil {
			return fmt.Errorf("convert %s: %w", id, err)
		}
		if len(list) == 0 {
			return fmt.Errorf("%s: models.dev provider %s produced no chat models", id, devID)
		}
		if err := models.CatalogDefaultOK(id, spec.DefaultID, list); err != nil {
			return err
		}
		providers[id] = list
	}
	out := struct {
		License   string                    `json:"license"`
		Copyright string                    `json:"copyright"`
		Source    string                    `json:"source"`
		SHA256    string                    `json:"sha256"`
		Providers map[string][]models.Model `json:"providers"`
	}{
		License:   "MIT",
		Copyright: "Copyright (c) 2025 models.dev",
		Source:    modelsDevURL,
		SHA256:    hex.EncodeToString(sum[:]),
		Providers: providers,
	}
	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	const path = "internal/models/catalog.json"
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d providers, sha256 %s)\n", path, len(providers), out.SHA256)
	return nil
}

func fetch(url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("user-agent", "pigo")
	req.Header.Set("accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
