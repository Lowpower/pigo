package llama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchHuggingFace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("search") != "qwen" || q.Get("filter") != "gguf" {
			t.Errorf("query=%v", q)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth=%q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "owner/qwen", "downloads": 42},
			{"id": "skip-me"},
		})
	}))
	t.Cleanup(srv.Close)
	got, err := SearchHuggingFace(context.Background(), "qwen", "tok", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "owner/qwen" || got[0].Downloads != 42 {
		t.Fatalf("%+v", got)
	}
	text := FormatSearch(got)
	if !strings.Contains(text, "owner/qwen") || !strings.Contains(text, "42 downloads") {
		t.Fatalf("%s", text)
	}
}

func TestFindHuggingFaceTokenEnv(t *testing.T) {
	t.Setenv("HF_TOKEN", "env-token")
	if got := FindHuggingFaceToken(); got != "env-token" {
		t.Fatalf("%q", got)
	}
}
