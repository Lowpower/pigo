package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamForCloudflareGatewaySwitchesPathByAPI(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case strings.Contains(r.URL.Path, "/responses"):
			_, _ = w.Write([]byte(responsesFixture))
		case strings.Contains(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(anthropicFixture))
		default:
			_, _ = w.Write([]byte(openAIFixture))
		}
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	req := Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	cfg := ClientConfig{
		APIKey: "k", BaseURL: srv.URL + "/v1/acct/gw/anthropic", HTTPClient: srv.Client(),
	}

	stream, err := StreamFor("cloudflare-ai-gateway", cfg)(ctx, req, Options{Model: "gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if !strings.Contains(gotPath, "/openai/") {
		t.Fatalf("gpt-5 path = %q, want /openai/", gotPath)
	}

	stream, err = StreamFor("cloudflare-ai-gateway", cfg)(ctx, req, Options{Model: "claude-sonnet-4"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if !strings.Contains(gotPath, "/anthropic/") {
		t.Fatalf("claude path = %q, want /anthropic/", gotPath)
	}

	stream, err = StreamFor("cloudflare-ai-gateway", cfg)(ctx, req, Options{Model: "llama-3"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if !strings.Contains(gotPath, "/compat/") {
		t.Fatalf("llama path = %q, want /compat/", gotPath)
	}
}

func TestStreamForCloudflareGatewayLeavesCustomBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	t.Cleanup(srv.Close)

	stream, err := StreamFor("cloudflare-ai-gateway", ClientConfig{
		APIKey: "k", BaseURL: srv.URL + "/custom", HTTPClient: srv.Client(),
	})(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if strings.Contains(gotPath, "/openai") {
		t.Fatalf("custom base rewritten: %q", gotPath)
	}
}

func TestCloudflareGatewayBaseURL(t *testing.T) {
	const root = "https://gateway.ai.cloudflare.com/v1/acct/gw"
	cases := []struct {
		api, base, want string
	}{
		{"openai-responses", root + "/anthropic", root + "/openai"},
		{"openai-completions", root + "/anthropic", root + "/compat"},
		{"anthropic-messages", root + "/openai", root + "/anthropic"},
		{"openai-responses", root + "/openai", root + "/openai"},
		{"openai-completions", root + "/compat/", root + "/compat"},
		{"openai-responses", "https://proxy.example/v1", "https://proxy.example/v1"},
		{"openai-responses", "", ""},
	}
	for _, tc := range cases {
		if got := cloudflareGatewayBaseURL(tc.api, tc.base); got != tc.want {
			t.Errorf("cloudflareGatewayBaseURL(%q, %q) = %q, want %q", tc.api, tc.base, got, tc.want)
		}
	}
}
