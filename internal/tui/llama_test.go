package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	if !transcriptContains(next.(Model), "/llama search") {
		t.Fatalf("usage missing search:\n%s", transcriptText(next.(Model)))
	}
}

func TestHandleLlamaSearch(t *testing.T) {
	m := New(testCfg())
	next, cmd := m.handleLlama("search qwen")
	got := next.(Model)
	if !transcriptContains(got, "searching Hugging Face") {
		t.Fatalf("pending:\n%s", transcriptText(got))
	}
	if cmd == nil {
		t.Fatal("expected async search")
	}
}

func drainCmds(m Model, cmd tea.Cmd) Model {
	for i := 0; i < 12 && cmd != nil; i++ {
		var next tea.Model
		next, cmd = m.Update(cmd())
		m = next.(Model)
	}
	return m
}

func TestHandleLlamaEmptyOpensManager(t *testing.T) {
	srv := llamaManagerServer(t, nil)
	t.Setenv("LLAMA_BASE_URL", srv.URL)
	m := New(testCfg())
	next, cmd := m.handleLlama("")
	got := next.(Model)
	if !got.llama.active {
		t.Fatal("empty /llama should open the manager overlay")
	}
	if cmd == nil {
		t.Fatal("expected catalog fetch")
	}
	got = drainCmds(got, cmd)
	view := got.llama.view()
	if !strings.Contains(view, "qwen") {
		t.Fatalf("catalog missing model:\n%s", view)
	}
	if !strings.Contains(view, "Download model") {
		t.Fatalf("catalog missing download:\n%s", view)
	}
}

func TestLlamaManagerDownloadSearchAndQuant(t *testing.T) {
	hf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/models" && r.URL.Query().Get("search") != "":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "owner/qwen", "downloads": 99},
			})
		case r.URL.Path == "/api/models/owner/qwen":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "owner/qwen",
				"gated": false,
				"siblings": []map[string]any{
					{"rfilename": "qwen-Q4_K_M.gguf", "size": 1000},
					{"rfilename": "qwen-Q8_0.gguf", "size": 2000},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hf.Close)
	var downloaded string
	srv := llamaManagerServer(t, func(model string) { downloaded = model })
	t.Setenv("LLAMA_BASE_URL", srv.URL)

	m := New(testCfg())
	m.llama.searchDebounce = time.Nanosecond
	m.llama.hfBaseURL = hf.URL
	next, cmd := m.handleLlama("")
	got := drainCmds(next.(Model), cmd)
	if got.llama.client != nil {
		got.llama.client.PollInterval = time.Millisecond
	}

	got.llama.list.selected = len(got.llama.list.filtered) - 1
	n, _ := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = n.(Model)
	if got.llama.phase != llamaSearch {
		t.Fatalf("phase=%v want search", got.llama.phase)
	}

	n, cmd = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qwen")})
	got = drainCmds(n.(Model), cmd)
	view := got.llama.view()
	if !strings.Contains(view, "owner/qwen") {
		t.Fatalf("search results:\n%s", view)
	}
	n, cmd = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = drainCmds(n.(Model), cmd)
	view = got.llama.view()
	if !strings.Contains(view, "Q4_K_M") {
		t.Fatalf("quant picker:\n%s", view)
	}
	n, cmd = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = drainCmds(n.(Model), cmd)
	if downloaded != "owner/qwen:Q4_K_M" {
		t.Fatalf("downloaded=%q view=\n%s", downloaded, got.llama.view())
	}
}

func TestLlamaManagerEscCloses(t *testing.T) {
	srv := llamaManagerServer(t, nil)
	t.Setenv("LLAMA_BASE_URL", srv.URL)
	m := New(testCfg())
	next, cmd := m.handleLlama("")
	got := drainCmds(next.(Model), cmd)
	got = send(got, tea.KeyMsg{Type: tea.KeyEsc})
	if got.llama.active {
		t.Fatal("esc should close overlay")
	}
}

func TestLlamaManagerConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LLAMA_BASE_URL", srv.URL)
	m := New(testCfg())
	next, cmd := m.handleLlama("")
	got := drainCmds(next.(Model), cmd)
	view := got.llama.view()
	if !strings.Contains(view, "Retry") || !strings.Contains(view, "Close") {
		t.Fatalf("connection error:\n%s", view)
	}
}

func llamaManagerServer(t *testing.T, onDownload func(string)) *httptest.Server {
	t.Helper()
	status := "unloaded"
	var extras []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			data := []map[string]any{
				{"id": "qwen", "status": map[string]any{"value": status}, "source": "preset"},
			}
			for _, id := range extras {
				data = append(data, map[string]any{
					"id": id, "status": map[string]any{"value": "unloaded"},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		case r.Method == http.MethodGet && r.URL.Path == "/props":
			_ = json.NewEncoder(w).Encode(map[string]any{"models_autoload": true})
		case r.Method == http.MethodPost && r.URL.Path == "/models/load":
			status = "loaded"
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/models/unload":
			status = "unloaded"
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/models":
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Model != "" {
				extras = append(extras, body.Model)
			}
			if onDownload != nil {
				onDownload(body.Model)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
