package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
				{"id": "pi-qwen", "name": "Qwen", "reasoning": true, "input": []string{"text"},
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
	m, ok := Lookup("radius", "pi-qwen")
	if !ok || m.API != "pi-messages" || m.BaseURL != "https://messages.example" {
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
