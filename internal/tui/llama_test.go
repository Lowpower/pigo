package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/llama"
	"github.com/Lowpower/pigo/internal/models"
)

func TestHandleLlamaLoadWaitsAndRefreshesCatalog(t *testing.T) {
	models.ClearOverlays()
	t.Cleanup(models.ClearOverlays)
	status := "unloaded"
	downloaded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			data := []map[string]any{
				{"id": "qwen", "status": map[string]any{"value": status}, "source": "preset"},
			}
			if downloaded {
				data = append(data, map[string]any{
					"id": "owner/repo:Q4_K_M", "status": map[string]any{"value": "unloaded"},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		case r.Method == http.MethodGet && r.URL.Path == "/props":
			_ = json.NewEncoder(w).Encode(map[string]any{"models_autoload": true})
		case r.Method == http.MethodPost && r.URL.Path == "/models/load":
			status = "loaded"
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/models":
			downloaded = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LLAMA_BASE_URL", srv.URL)

	m := New(testCfg())
	next, cmd := m.handleLlama("load qwen")
	got := next.(Model)
	if cmd != nil {
		got = send(got, cmd())
	}
	if !transcriptContains(got, "loaded qwen") {
		t.Fatalf("load transcript:\n%s", transcriptText(got))
	}
	found := false
	for _, model := range models.Catalog() {
		if model.Provider == llama.ProviderID && model.ID == "qwen" {
			found = true
		}
	}
	if !found {
		t.Fatal("catalog was not refreshed after load")
	}

	next, cmd = got.handleLlama("download owner/repo:Q4_K_M")
	got = next.(Model)
	if cmd != nil {
		got = send(got, cmd())
	}
	if !transcriptContains(got, "downloaded") {
		t.Fatalf("download transcript:\n%s", transcriptText(got))
	}
}

func transcriptText(m Model) string {
	var b strings.Builder
	for _, e := range m.transcript {
		b.WriteString(e.rendered)
		b.WriteByte('\n')
	}
	return b.String()
}

func transcriptContains(m Model, want string) bool {
	return strings.Contains(transcriptText(m), want)
}

func TestHandleLlamaUsageMentionsDownload(t *testing.T) {
	m := New(testCfg())
	next, _ := m.handleLlama("nope")
	if !transcriptContains(next.(Model), "/llama download") {
		t.Fatalf("usage:\n%s", transcriptText(next.(Model)))
	}
}
