package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestBuildOpenAIRequestSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-ai", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-ai", ID: "m", API: "openai-completions",
			SamplingParams: map[string]any{
				"temperature": 1,
				"top_p":       0.9,
				"top_k":       0,
				"max_tokens":  7,
				"min_p":       nil,
			},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-ai") })

	body, err := buildOpenAIRequest(Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{
		Provider:  "samp-ai",
		Model:     "m",
		MaxTokens: 100,
		SamplingParams: map[string]any{
			"temperature": 0,
			"max_tokens":  5,
			"vendor_flag": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["samplingParams"]; ok {
		t.Fatalf("samplingParams nested in body: %#v", payload["samplingParams"])
	}
	if payload["temperature"] != float64(0) {
		t.Fatalf("temperature = %#v, want caller 0", payload["temperature"])
	}
	if payload["top_p"] != 0.9 {
		t.Fatalf("top_p = %#v", payload["top_p"])
	}
	if payload["top_k"] != float64(0) {
		t.Fatalf("top_k = %#v, want 0", payload["top_k"])
	}
	if payload["max_tokens"] != float64(100) {
		t.Fatalf("max_tokens = %#v, want caller 100", payload["max_tokens"])
	}
	if payload["vendor_flag"] != false {
		t.Fatalf("vendor_flag = %#v", payload["vendor_flag"])
	}
	if _, present := payload["min_p"]; !present {
		t.Fatal("null min_p dropped")
	}

	emptyBody, err := buildOpenAIRequest(Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	var empty map[string]any
	if err := json.Unmarshal(emptyBody, &empty); err != nil {
		t.Fatal(err)
	}
	if _, ok := empty["temperature"]; ok {
		t.Fatalf("unset sampling leaked: %#v", empty["temperature"])
	}
	if _, ok := empty["samplingParams"]; ok {
		t.Fatal("empty samplingParams object was sent")
	}
}

func TestBuildOpenAIRequestIgnoresNonOpenAISampling(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-ant", DefaultAPI: "anthropic-messages", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-ant", ID: "m", API: "anthropic-messages",
			SamplingParams: map[string]any{"top_p": 0.2},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-ant") })

	body, err := buildOpenAIRequest(Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{
		Provider:       "samp-ant",
		Model:          "m",
		SamplingParams: map[string]any{"temperature": 0.3},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["top_p"]; ok {
		t.Fatalf("anthropic model sampling leaked: %#v", payload["top_p"])
	}
	if payload["temperature"] != 0.3 {
		t.Fatalf("caller temperature = %#v", payload["temperature"])
	}
}

func TestOpenAICompletionsStreamSendsSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-stream", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-stream", ID: "m", API: "openai-completions",
			SamplingParams: map[string]any{
				"temperature":    0.2,
				"top_k":          0,
				"max_tokens":     9,
				"dry_multiplier": 0.8,
			},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-stream") })

	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"))
	}))
	defer srv.Close()

	client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Provider: "samp-stream", Model: "m", MaxTokens: 64})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "ok" {
		t.Fatalf("final = %v", final)
	}
	if payload["temperature"] != 0.2 {
		t.Fatalf("temperature = %#v", payload["temperature"])
	}
	if payload["top_k"] != float64(0) {
		t.Fatalf("top_k = %#v", payload["top_k"])
	}
	if payload["dry_multiplier"] != 0.8 {
		t.Fatalf("dry_multiplier = %#v", payload["dry_multiplier"])
	}
	if payload["max_tokens"] != float64(64) {
		t.Fatalf("max_tokens = %#v, want 64", payload["max_tokens"])
	}
}

func TestOpenAIResponsesSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-resp", DefaultAPI: "openai-responses", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-resp", ID: "m", API: "openai-responses",
			SamplingParams: map[string]any{
				"temperature":       1,
				"top_p":             0.4,
				"max_output_tokens": 9,
				"vendor_flag":       false,
			},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-resp") })

	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	defer srv.Close()

	client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{
		Provider:  "samp-resp",
		Model:     "m",
		MaxTokens: 32,
		Thinking:  "low",
		SamplingParams: map[string]any{
			"temperature": 0,
			"top_k":       0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if payload["temperature"] != float64(0) {
		t.Fatalf("temperature = %#v", payload["temperature"])
	}
	if payload["top_p"] != 0.4 {
		t.Fatalf("top_p = %#v", payload["top_p"])
	}
	if payload["top_k"] != float64(0) {
		t.Fatalf("top_k = %#v", payload["top_k"])
	}
	if payload["vendor_flag"] != false {
		t.Fatalf("vendor_flag = %#v", payload["vendor_flag"])
	}
	if payload["max_output_tokens"] != float64(32) {
		t.Fatalf("max_output_tokens = %#v, want 32", payload["max_output_tokens"])
	}
	reasoning, _ := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "low" {
		t.Fatalf("reasoning = %#v", payload["reasoning"])
	}
	if payload["store"] != false {
		t.Fatalf("store = %#v", payload["store"])
	}
}
