package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshRadiusLoadsConfig(t *testing.T) {
	t.Cleanup(ClearOverlays)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/config" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("authorization") != "Bearer rk" {
			t.Errorf("auth = %q", r.Header.Get("authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseUrl": "https://messages.example",
			"models": []map[string]any{
				{"id": "qwen", "name": "Qwen", "reasoning": true, "input": []string{"text"},
					"cost":          map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
					"contextWindow": 128000, "maxTokens": 8192},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)
	t.Setenv("RADIUS_API_KEY", "rk")
	store := &MemoryStore{}
	if err := refreshRadius(store); err != nil {
		t.Fatal(err)
	}
	m, ok := Lookup("radius", "qwen")
	if !ok || m.API != "pigo-messages" || m.BaseURL != "https://messages.example" {
		t.Fatalf("model = %+v ok=%v", m, ok)
	}
}

func TestRefreshRadiusSkipsWithoutAuthOrGateway(t *testing.T) {
	t.Setenv("RADIUS_GATEWAY", "")
	t.Setenv("RADIUS_API_KEY", "")
	if err := refreshRadius(&MemoryStore{}); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForRadiusCatalogLoadsImmediately(t *testing.T) {
	t.Cleanup(ClearOverlays)
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseUrl": "https://messages.example",
			"models":  []map[string]any{{"id": "balanced"}},
		})
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)
	t.Setenv("RADIUS_API_KEY", "")
	store := &MemoryStore{}
	ok := WaitForRadiusCatalog(t.Context(), store, "oauth-tok", nil)
	if !ok {
		t.Fatal("expected catalog ready")
	}
	if gotAuth.Load() != "Bearer oauth-tok" {
		t.Fatalf("authorization = %v", gotAuth.Load())
	}
	if _, found := Lookup("radius", "balanced"); !found {
		t.Fatal("balanced missing from catalog")
	}
	if _, found, _ := store.Read("radius"); !found {
		t.Fatal("expected models-store write")
	}
}

func TestWaitForRadiusCatalogRetriesUntilReady(t *testing.T) {
	t.Cleanup(ClearOverlays)
	origRetry := radiusCatalogRetry
	radiusCatalogRetry = 10 * time.Millisecond
	t.Cleanup(func() { radiusCatalogRetry = origRetry })

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseUrl": "https://messages.example",
			"models":  []map[string]any{{"id": "balanced"}},
		})
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)
	ok := WaitForRadiusCatalog(t.Context(), &MemoryStore{}, "tok", nil)
	if !ok {
		t.Fatal("expected catalog after retries")
	}
	if n.Load() < 3 {
		t.Fatalf("hits = %d", n.Load())
	}
	if _, found := Lookup("radius", "balanced"); !found {
		t.Fatal("balanced missing after retry")
	}
}

func TestWaitForRadiusCatalogTimeout(t *testing.T) {
	t.Cleanup(ClearOverlays)
	origRetry := radiusCatalogRetry
	radiusCatalogRetry = 10 * time.Millisecond
	t.Cleanup(func() { radiusCatalogRetry = origRetry })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseUrl": "https://messages.example",
			"models":  []map[string]any{},
		})
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)
	var notes []string
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Millisecond)
	defer cancel()
	ok := WaitForRadiusCatalog(ctx, &MemoryStore{}, "tok", func(msg string) { notes = append(notes, msg) })
	if ok {
		t.Fatal("empty catalog should time out")
	}
	if _, found := Lookup("radius", "balanced"); !found {
		t.Fatal("public catalog should stay available when the gateway returns no models")
	}
	if len(remoteOverlay("radius")) != 0 {
		t.Fatal("empty gateway catalog should not become the overlay")
	}
	foundTimeout := false
	for _, n := range notes {
		if n != "" {
			foundTimeout = true
			break
		}
	}
	if !foundTimeout {
		t.Fatalf("expected timeout notify, got %v", notes)
	}
}

func TestPublicRadiusCatalogWithoutGateway(t *testing.T) {
	t.Setenv("RADIUS_GATEWAY", "")
	t.Setenv("PIGO_RADIUS_GATEWAY", "")
	for _, id := range []string{"balanced", "cheap", "precise"} {
		m, ok := Lookup("radius", id)
		if !ok || m.API != "pigo-messages" || m.BaseURL == "" {
			t.Fatalf("%s = %+v ok=%v", id, m, ok)
		}
	}
	balanced, _ := Lookup("radius", "balanced")
	if balanced.ContextWindow != 1048576 || balanced.Name != "Balanced" {
		t.Fatalf("balanced = %+v", balanced)
	}
	store := &MemoryStore{}
	if err := refreshRadius(store); err != nil {
		t.Fatal(err)
	}
	entry, found, err := store.Read("radius")
	if err != nil || !found || len(entry.Models) < 30 {
		t.Fatalf("store models = %d found=%v err=%v", len(entry.Models), found, err)
	}
	if err := store.Write("radius", StoreEntry{Models: []Model{{ID: "keep-me"}}}); err != nil {
		t.Fatal(err)
	}
	if err := refreshRadius(store); err != nil {
		t.Fatal(err)
	}
	entry, found, err = store.Read("radius")
	if err != nil || !found || len(entry.Models) != 1 || entry.Models[0].ID != "keep-me" {
		t.Fatalf("cache overwritten: %+v found=%v err=%v", entry, found, err)
	}
}

func TestWaitForRadiusCatalogWithoutGateway(t *testing.T) {
	t.Setenv("RADIUS_GATEWAY", "")
	t.Setenv("PIGO_RADIUS_GATEWAY", "")
	var notes []string
	ok := WaitForRadiusCatalog(t.Context(), &MemoryStore{}, "", func(msg string) {
		notes = append(notes, msg)
	})
	if !ok {
		t.Fatal("public catalog should be ready without a gateway")
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %v", notes)
	}
}

func TestRefreshRadiusOverlaysPublicCatalog(t *testing.T) {
	t.Cleanup(ClearOverlays)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/config" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseUrl": "https://messages.example/v1",
			"models": []map[string]any{
				{"id": "balanced", "name": "Balanced Live", "contextWindow": 111, "reasoning": false, "input": []string{"text"}},
				{"id": "gw-only", "name": "Gateway Only", "contextWindow": 222},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)
	t.Setenv("RADIUS_API_KEY", "")
	store := &MemoryStore{}
	if err := refreshRadius(store); err != nil {
		t.Fatal(err)
	}
	balanced, ok := Lookup("radius", "balanced")
	if !ok || balanced.BaseURL != "https://messages.example/v1" || balanced.Name != "Balanced Live" || balanced.ContextWindow != 111 {
		t.Fatalf("balanced = %+v ok=%v", balanced, ok)
	}
	if balanced.Reasoning == nil || *balanced.Reasoning {
		t.Fatalf("reasoning = %v", balanced.Reasoning)
	}
	if _, ok := Lookup("radius", "cheap"); !ok {
		t.Fatal("public id missing after overlay")
	}
	gw, ok := Lookup("radius", "gw-only")
	if !ok || gw.API != "pigo-messages" || gw.ContextWindow != 222 {
		t.Fatalf("gw-only = %+v ok=%v", gw, ok)
	}
	entry, found, err := store.Read("radius")
	if err != nil || !found || len(entry.Models) != 2 {
		t.Fatalf("store = %+v found=%v err=%v", entry.Models, found, err)
	}
}

func TestCustomRadiusGatewaySkipsPublicCatalog(t *testing.T) {
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("radius-dev")
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseUrl": "http://localhost:8788/v1",
			"models":  []map[string]any{{"id": "auto", "name": "Radius Auto"}},
		})
	}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
	  "providers": {
	    "radius-dev": {"name": "Radius (dev)", "baseUrl": "` + srv.URL + `", "oauth": "radius"}
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("radius-dev", "balanced"); ok {
		t.Fatal("custom gateway inherited the public catalog")
	}
	if _, ok := Lookup("radius", "balanced"); !ok {
		t.Fatal("builtin public catalog dropped")
	}
	spec, ok := LookupProvider("radius-dev")
	if !ok || spec.RadiusGateway != srv.URL || len(spec.Models) != 0 || spec.RefreshModels == nil {
		t.Fatalf("spec = %+v ok=%v", spec, ok)
	}
	store := &MemoryStore{}
	if err := spec.RefreshModels(store); err != nil {
		t.Fatal(err)
	}
	m, ok := Lookup("radius-dev", "auto")
	if !ok || m.BaseURL != "http://localhost:8788/v1" || m.API != "pigo-messages" {
		t.Fatalf("auto = %+v ok=%v", m, ok)
	}
	if _, ok := Lookup("radius-dev", "balanced"); ok {
		t.Fatal("refresh attached the public catalog")
	}
	entry, found, err := store.Read("radius-dev")
	if err != nil || !found || len(entry.Models) != 1 || entry.Models[0].ID != "auto" {
		t.Fatalf("store = %+v found=%v err=%v", entry, found, err)
	}
	if _, found, _ := store.Read("radius"); found {
		t.Fatal("custom refresh wrote the builtin radius store")
	}
}

func TestCustomRadiusGatewayReplacesBuiltinCatalog(t *testing.T) {
	orig, ok := LookupProvider("radius")
	if !ok {
		t.Fatal("radius missing")
	}
	t.Cleanup(func() {
		RegisterProvider(orig)
		ClearOverlays()
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{"providers":{"radius":{"baseUrl":"http://127.0.0.1:9","oauth":"radius"}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("radius", "balanced"); ok {
		t.Fatal("overridden radius inherited the public catalog")
	}
	spec, _ := LookupProvider("radius")
	if spec.RadiusGateway != "http://127.0.0.1:9" || len(spec.Models) != 0 {
		t.Fatalf("spec gateway=%q models=%d", spec.RadiusGateway, len(spec.Models))
	}
}

func TestLoadUserJSONRadiusOAuthRequiresBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{"providers":{"radius-dev":{"oauth":"radius"}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserJSON(path); err == nil {
		t.Fatal("expected missing baseUrl error")
	}
}

func TestPrepareCatalogSeedsPublicRadiusOffline(t *testing.T) {
	t.Setenv("RADIUS_GATEWAY", "")
	t.Setenv("PIGO_RADIUS_GATEWAY", "")
	dir := t.TempDir()
	if err := PrepareCatalog(dir, "http://127.0.0.1:1", true); err != nil {
		t.Fatal(err)
	}
	entry, found, err := OpenFileStore(filepath.Join(dir, "models-store.json")).Read("radius")
	if err != nil || !found {
		t.Fatalf("store found=%v err=%v", found, err)
	}
	seen := false
	for _, m := range entry.Models {
		if m.ID == "balanced" && m.API == "pigo-messages" {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatal("offline seed missing balanced")
	}
}
