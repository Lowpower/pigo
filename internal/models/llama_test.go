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
