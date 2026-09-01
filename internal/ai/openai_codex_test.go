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

func TestResolveCodexURL(t *testing.T) {
	if got := resolveCodexURL(""); got != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("%s", got)
	}
	if got := resolveCodexURL("https://chatgpt.com/backend-api"); got != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("%s", got)
	}
	if got := resolveCodexURL("https://example/codex"); got != "https://example/codex/responses" {
		t.Fatalf("%s", got)
	}
}

func TestOpenAICodexClientHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/codex/responses") {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("originator") != "pi" {
			t.Errorf("originator = %q", r.Header.Get("originator"))
		}
		if r.Header.Get("OpenAI-Beta") != "responses=experimental" {
			t.Errorf("beta = %q", r.Header.Get("OpenAI-Beta"))
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["store"] != false {
			t.Errorf("store = %v", payload["store"])
		}
		if payload["stream"] != true {
			t.Errorf("stream = %v", payload["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	client := &OpenAICodexClient{
		BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(),
		Headers: map[string]string{"originator": "pi", "OpenAI-Beta": "responses=experimental"},
	}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-5.3-codex-spark"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if final.API != "openai-codex-responses" {
		t.Fatalf("api = %q", final.API)
	}
}

func TestStreamForOpenAICodex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("chatgpt-account-id") != "acct-1" {
			t.Errorf("account = %q", r.Header.Get("chatgpt-account-id"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()
	stream, err := StreamFor("openai-codex", ClientConfig{
		APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(),
		Headers: map[string]string{"chatgpt-account-id": "acct-1"},
	})(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-5.3-codex-spark"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("%+v", final)
	}
}
