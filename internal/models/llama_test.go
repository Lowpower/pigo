package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Lowpower/pigo/internal/llama"
)

func TestApplyLlamaCatalogOverlaysSelectableModels(t *testing.T) {
	ClearOverlays()
	t.Cleanup(ClearOverlays)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "qwen", "status": map[string]any{"value": "loaded"}},
					{"id": "hidden", "status": map[string]any{"value": "unloaded"}},
				},
			})
		case "/props":
			_ = json.NewEncoder(w).Encode(map[string]any{"models_autoload": false})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := llama.NewClient(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyLlamaCatalog(c, nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range Catalog() {
		if m.Provider == llama.ProviderID && m.ID == "qwen" {
			found = true
		}
		if m.ID == "hidden" {
			t.Fatalf("unloaded non-autoload model should not overlay: %+v", m)
		}
	}
	if !found {
		t.Fatal("loaded model missing from catalog overlay")
	}
}

func TestApplyLlamaCatalogWritesContextWindow(t *testing.T) {
	ClearOverlays()
	t.Cleanup(ClearOverlays)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "from-meta", "status": map[string]any{"value": "loaded"}, "meta": map[string]any{"n_ctx": 32768, "n_ctx_train": 4096}},
					{"id": "from-train", "status": map[string]any{"value": "loaded"}, "meta": map[string]any{"n_ctx_train": 8192}},
					{"id": "from-args", "status": map[string]any{"value": "loaded", "args": []string{"--ctx-size", "16384"}}},
				},
			})
		case "/props":
			_ = json.NewEncoder(w).Encode(map[string]any{"models_autoload": false})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := llama.NewClient(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyLlamaCatalog(c, nil); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, m := range Catalog() {
		if m.Provider == llama.ProviderID {
			got[m.ID] = m.ContextWindow
		}
	}
	if got["from-meta"] != 32768 || got["from-train"] != 8192 || got["from-args"] != 16384 {
		t.Fatalf("context windows = %#v", got)
	}
}
