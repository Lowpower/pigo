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

func TestParseHuggingFaceModel(t *testing.T) {
	repo, quant := ParseHuggingFaceModel("owner/repo:Q4_K_M")
	if repo != "owner/repo" || quant != "Q4_K_M" {
		t.Fatalf("repo=%q quant=%q", repo, quant)
	}
	repo, quant = ParseHuggingFaceModel("owner/repo")
	if repo != "owner/repo" || quant != "" {
		t.Fatalf("repo=%q quant=%q", repo, quant)
	}
}

func TestHuggingFaceDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/owner/repo" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("blobs") != "true" {
			t.Errorf("query=%v", r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "owner/repo",
			"gated": "auto",
			"siblings": []map[string]any{
				{"rfilename": "model-Q4_K_M.gguf", "size": 1000},
				{"rfilename": "model-Q8_0.gguf", "size": 2000},
				{"rfilename": "mmproj-F16.gguf", "size": 50},
				{"rfilename": "model-Q4_K_M-00001-of-00002.gguf", "size": 500},
			},
		})
	}))
	t.Cleanup(srv.Close)
	got, err := HuggingFaceDetails(context.Background(), "owner/repo", "tok", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "owner/repo" || got.Gated != "auto" {
		t.Fatalf("%+v", got)
	}
	if len(got.Quantizations) < 2 || got.Quantizations[0].Name != "Q4_K_M" {
		t.Fatalf("quants=%+v", got.Quantizations)
	}
}

func TestFormatBytes(t *testing.T) {
	if got := FormatBytes(500); got != "500 B" {
		t.Fatalf("%q", got)
	}
	if got := FormatBytes(2048); !strings.Contains(got, "KiB") {
		t.Fatalf("%q", got)
	}
}
