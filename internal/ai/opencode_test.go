package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestOpenCodeRoutesByModel verifies the opencode provider dispatches Claude
// models to the Anthropic Messages endpoint (x-api-key) and other models to the
// OpenAI Chat Completions endpoint (Bearer), against one fake gateway.
func TestOpenCodeRoutesByModel(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]string{} // path -> auth header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		switch {
		case r.Header.Get("x-api-key") != "":
			hits[r.URL.Path] = "x-api-key"
		default:
			hits[r.URL.Path] = r.Header.Get("authorization")
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		switch r.URL.Path {
		case "/v1/messages":
			_, _ = w.Write([]byte(anthropicFixture))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(openAIFixture))
		case "/v1/responses":
			_, _ = w.Write([]byte(responsesFixture))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("OPENCODE_API_KEY", "zen-key")
	t.Setenv("OPENCODE_BASE_URL", srv.URL)

	sf, ok := NewOpenCodeFromEnv()
	if !ok {
		t.Fatal("NewOpenCodeFromEnv returned ok=false with OPENCODE_API_KEY set")
	}

	// Claude model -> Anthropic Messages endpoint with x-api-key.
	streamA, err := sf(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "claude-sonnet-4"})
	if err != nil {
		t.Fatalf("claude stream error: %v", err)
	}
	_, finalA := streamA.Collect()
	if finalA == nil || finalA.Text() != "Hello, world" {
		t.Fatalf("claude final = %+v", finalA)
	}

	// Non-Claude model -> Chat Completions endpoint with Bearer.
	streamB, err := sf(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "deepseek-v4"})
	if err != nil {
		t.Fatalf("deepseek stream error: %v", err)
	}
	_, finalB := streamB.Collect()
	if finalB == nil || finalB.Text() != "Hello, world" {
		t.Fatalf("deepseek final = %+v", finalB)
	}

	// GPT-5 -> OpenAI Responses endpoint with Bearer.
	streamC, err := sf(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("gpt-5 stream error: %v", err)
	}
	_, finalC := streamC.Collect()
	if finalC == nil || finalC.Text() != "Hello, world" {
		t.Fatalf("gpt-5 final = %+v", finalC)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits["/v1/messages"] != "x-api-key" {
		t.Errorf("claude did not hit /v1/messages with x-api-key; hits=%v", hits)
	}
	if hits["/v1/chat/completions"] != "Bearer zen-key" {
		t.Errorf("deepseek did not hit /v1/chat/completions with Bearer; hits=%v", hits)
	}
	if hits["/v1/responses"] != "Bearer zen-key" {
		t.Errorf("gpt-5 did not hit /v1/responses with Bearer; hits=%v", hits)
	}
}
