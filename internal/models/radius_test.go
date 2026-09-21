package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
					"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
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

func TestRadiusGatewayDefaultsToPublicHost(t *testing.T) {
	t.Setenv("RADIUS_GATEWAY", "")
	t.Setenv("PIGO_RADIUS_GATEWAY", "")
	if RadiusGateway() != defaultRadiusGateway {
		t.Fatalf("gateway = %q", RadiusGateway())
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
		t.Fatal("offline fallback balanced should remain listed")
	}
	if len(remoteOverlay("radius")) != 0 {
		t.Fatal("empty gateway config should not overlay models")
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
