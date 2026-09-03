package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseOpenRouterImages(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/png;base64,AAA"}}]}}]}`)
	got, err := parseOpenRouterImages(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MimeType != "image/png" || got[0].Data != "AAA" {
		t.Fatalf("%+v", got)
	}
}

func TestGenerateOpenRouterImages(t *testing.T) {
	var gotAuth, gotModel string
	var modalities []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authorization")
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		gotModel, _ = req["model"].(string)
		raw, _ := req["modalities"].([]any)
		for _, v := range raw {
			s, _ := v.(string)
			modalities = append(modalities, s)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"images":[{"image_url":"data:image/png;base64,QQQ"}]}}]}`))
	}))
	defer srv.Close()

	t.Setenv("OPENROUTER_API_KEY", "or-key")
	t.Setenv("OPENROUTER_BASE_URL", srv.URL+"/v1")
	got, err := generateOpenRouterImages(context.Background(), "a cat", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer or-key" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotModel != DefaultImagesModel {
		t.Fatalf("model=%q", gotModel)
	}
	if strings.Join(modalities, ",") != "image" {
		t.Fatalf("modalities=%v", modalities)
	}
	if len(got) != 1 || got[0].Data != "QQQ" {
		t.Fatalf("%+v", got)
	}
}

func TestGenerateOpenRouterImagesRequiresKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	if _, err := generateOpenRouterImages(context.Background(), "hi", ""); err == nil {
		t.Fatal("expected missing key")
	}
}
