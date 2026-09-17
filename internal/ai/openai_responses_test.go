package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

const responsesFixture = `event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","delta":"Hello","content_index":0,"output_index":0,"sequence_number":1,"logprobs":[]}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_1","delta":", world","content_index":0,"output_index":0,"sequence_number":2,"logprobs":[]}

event: response.completed
data: {"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

`

func TestOpenAIResponsesClientHTTP(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			t.Errorf("missing Bearer authorization, got %q", r.Header.Get("authorization"))
		}
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Errorf("path = %q, want .../responses", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("body json: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-test", Thinking: "high", SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if final.API != "openai-responses" {
		t.Fatalf("api = %q", final.API)
	}
	if final.Usage.Input != 4 || final.Usage.Output != 2 {
		t.Fatalf("usage = %+v", final.Usage)
	}
	if payload["store"] != false {
		t.Errorf("store = %#v", payload["store"])
	}
	if payload["prompt_cache_key"] != "sess-1" {
		t.Errorf("prompt_cache_key = %#v", payload["prompt_cache_key"])
	}
	reasoning, _ := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning = %#v", payload["reasoning"])
	}
	include, _ := payload["include"].([]any)
	if len(include) == 0 || include[0] != "reasoning.encrypted_content" {
		t.Errorf("include = %#v", payload["include"])
	}
}

func TestOpenAIResponsesPromptCacheOptions(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Provider: "openai", Model: "gpt-6-astra", CacheRetention: "long", SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	opts, _ := payload["prompt_cache_options"].(map[string]any)
	if opts["ttl"] != "30m" {
		t.Fatalf("prompt_cache_options = %#v payload=%#v", opts, payload)
	}
	if _, ok := payload["prompt_cache_retention"]; ok {
		t.Fatalf("should not send 24h retention with explicit cache mode: %#v", payload)
	}
}

func TestStreamForAzureOpenAIResponses(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "gpt-4=my-dep")
	var gotModel, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotVersion = r.Header.Get("api-version")
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		gotModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	stream, err := StreamFor("azure-openai-responses", ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-4"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if final.API != "azure-openai-responses" {
		t.Fatalf("api = %q", final.API)
	}
	if gotVersion != "v1" {
		t.Fatalf("api-version = %q", gotVersion)
	}
	if gotModel != "my-dep" {
		t.Fatalf("model = %q, want my-dep", gotModel)
	}
}

func TestBuildResponsesInputToolPair(t *testing.T) {
	items := buildResponsesInput(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "c1", ToolName: "read", Arguments: map[string]any{"path": "a"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "c1", Content: "ok"},
	}})
	if len(items) < 3 {
		t.Fatalf("items = %d", len(items))
	}
}

func TestOpenAIResponsesSessionAffinityHeaders(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "aff-resp", DefaultAPI: "openai-responses", DefaultID: "none",
		Models: []models.Model{
			{Provider: "aff-resp", ID: "openai", Compat: &models.Compat{SessionAffinityFormat: "openai"}},
			{Provider: "aff-resp", ID: "nosession", Compat: &models.Compat{SessionAffinityFormat: "openai-nosession"}},
			{Provider: "aff-resp", ID: "openrouter", Compat: &models.Compat{SessionAffinityFormat: "openrouter"}},
			{Provider: "aff-resp", ID: "none"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("aff-resp") })

	cases := []struct {
		model string
		want  map[string]string
		dont  []string
	}{
		{
			model: "openai",
			want: map[string]string{
				"session_id":          "sess-1",
				"x-client-request-id": "sess-1",
			},
			dont: []string{"x-session-id", "x-session-affinity"},
		},
		{
			model: "nosession",
			want:  map[string]string{"x-client-request-id": "sess-1"},
			dont:  []string{"session_id", "x-session-id", "x-session-affinity"},
		},
		{
			model: "openrouter",
			want:  map[string]string{"x-session-id": "sess-1"},
			dont:  []string{"session_id", "x-client-request-id", "x-session-affinity"},
		},
		{
			model: "none",
			dont:  []string{"session_id", "x-session-id", "x-session-affinity", "x-client-request-id"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			var got http.Header
			var payload map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &payload)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(responsesFixture))
			}))
			defer srv.Close()

			client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: "aff-resp", Model: tc.model, SessionID: "sess-1"})
			if err != nil {
				t.Fatal(err)
			}
			stream.Collect()
			for k, v := range tc.want {
				if got.Get(k) != v {
					t.Errorf("%s = %q, want %q", k, got.Get(k), v)
				}
			}
			for _, k := range tc.dont {
				if got.Get(k) != "" {
					t.Errorf("unexpected %s = %q", k, got.Get(k))
				}
			}
			if payload["prompt_cache_key"] != "sess-1" {
				t.Errorf("prompt_cache_key = %#v", payload["prompt_cache_key"])
			}
		})
	}
}

func TestOpenAIResponsesMaxOutputTokensCompat(t *testing.T) {
	omit, send := false, true
	models.RegisterProvider(models.ProviderSpec{
		ID: "maxout-resp", DefaultAPI: "openai-responses", DefaultID: "unset",
		Models: []models.Model{
			{Provider: "maxout-resp", ID: "omit", Compat: &models.Compat{SupportsMaxOutputTokens: &omit}},
			{Provider: "maxout-resp", ID: "send", Compat: &models.Compat{SupportsMaxOutputTokens: &send}},
			{Provider: "maxout-resp", ID: "unset"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("maxout-resp") })

	cases := []struct {
		name, model string
		maxTokens   int
		wantPresent bool
		want        int
	}{
		{name: "false omits", model: "omit", maxTokens: 100},
		{name: "true sends", model: "send", maxTokens: 100, wantPresent: true, want: 100},
		{name: "true clamps below 16", model: "send", maxTokens: 10, wantPresent: true, want: 16},
		{name: "unset sends", model: "unset", maxTokens: 100, wantPresent: true, want: 100},
		{name: "false without MaxTokens omits", model: "omit"},
		{name: "unset without MaxTokens omits", model: "unset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &payload)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(responsesFixture))
			}))
			defer srv.Close()

			client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
			stream, err := client.StreamFn()(context.Background(), Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, Options{Provider: "maxout-resp", Model: tc.model, MaxTokens: tc.maxTokens})
			if err != nil {
				t.Fatal(err)
			}
			stream.Collect()

			got, ok := payload["max_output_tokens"]
			if !tc.wantPresent {
				if ok {
					t.Fatalf("unexpected max_output_tokens = %#v", got)
				}
				return
			}
			if !ok {
				t.Fatalf("missing max_output_tokens in %#v", payload)
			}
			n, _ := got.(float64)
			if int(n) != tc.want {
				t.Fatalf("max_output_tokens = %#v, want %d", got, tc.want)
			}
		})
	}
}
