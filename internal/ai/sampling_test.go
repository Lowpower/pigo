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
	if payload["max_completion_tokens"] != float64(100) {
		t.Fatalf("max_completion_tokens = %#v, want caller 100", payload["max_completion_tokens"])
	}
	if _, ok := payload["max_tokens"]; ok {
		t.Fatalf("sampling max_tokens leaked: %#v", payload["max_tokens"])
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
	if payload["max_completion_tokens"] != float64(64) {
		t.Fatalf("max_completion_tokens = %#v, want 64", payload["max_completion_tokens"])
	}
	if _, ok := payload["max_tokens"]; ok {
		t.Fatalf("sampling max_tokens leaked: %#v", payload["max_tokens"])
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

func TestResolveSamplingParamsByThinkingLevel(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-level", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{
			{
				Provider: "samp-level", ID: "m", API: "openai-completions",
				SamplingParams: map[string]any{
					"temperature": 1,
					"top_p":       0.95,
					"top_k":       20,
				},
				SamplingParamsByThinkingLevel: map[string]map[string]any{
					"off":  {"temperature": 0.7},
					"low":  {"temperature": 0.2},
					"high": {"temperature": 0.8, "top_p": 0.5},
					"max":  nil,
				},
			},
			{
				Provider: "samp-level", ID: "plain", API: "openai-completions",
				SamplingParams: map[string]any{"temperature": 1, "top_p": 0.9},
			},
			{
				Provider: "samp-level", ID: "null-high", API: "openai-completions",
				SamplingParams: map[string]any{"temperature": 1, "top_p": 0.9},
				SamplingParamsByThinkingLevel: map[string]map[string]any{
					"high": nil,
					"low":  {"temperature": 0.2},
				},
			},
			{
				Provider: "samp-level", ID: "clamp", API: "openai-completions",
				ThinkingLevelMap: map[string]*string{"off": nil, "minimal": nil},
				SamplingParams:   map[string]any{"temperature": 1, "top_p": 0.9},
				SamplingParamsByThinkingLevel: map[string]map[string]any{
					"off": {"temperature": 0.1},
					"low": {"temperature": 0.4},
				},
			},
			{
				Provider: "samp-level", ID: "ant", API: "anthropic-messages",
				SamplingParams: map[string]any{"temperature": 1, "top_p": 0.2},
				SamplingParamsByThinkingLevel: map[string]map[string]any{
					"off":  {"temperature": 0.7},
					"high": {"temperature": 0.1},
				},
			},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-level") })

	low := ResolveSamplingParams("samp-level", "m", "low", nil)
	if low["temperature"] != 0.2 || low["top_p"] != 0.95 || low["top_k"] != 20 {
		t.Fatalf("low = %#v", low)
	}
	high := ResolveSamplingParams("samp-level", "m", "high", map[string]any{"temperature": 0})
	if high["temperature"] != 0 || high["top_p"] != 0.5 || high["top_k"] != 20 {
		t.Fatalf("caller high = %#v", high)
	}
	again := ResolveSamplingParams("samp-level", "m", "high", high)
	if again["temperature"] != 0 || again["top_p"] != 0.5 || again["top_k"] != 20 {
		t.Fatalf("second resolve = %#v", again)
	}
	medium := ResolveSamplingParams("samp-level", "m", "medium", nil)
	if medium["temperature"] != 1 || medium["top_p"] != 0.95 || medium["top_k"] != 20 {
		t.Fatalf("missing level = %#v", medium)
	}
	empty := ResolveSamplingParams("samp-level", "m", "", nil)
	if empty["temperature"] != 0.7 || empty["top_p"] != 0.95 || empty["top_k"] != 20 {
		t.Fatalf("empty thinking = %#v", empty)
	}
	nullHigh := ResolveSamplingParams("samp-level", "null-high", "high", nil)
	if nullHigh["temperature"] != 1 || nullHigh["top_p"] != 0.9 {
		t.Fatalf("null high = %#v", nullHigh)
	}
	plain := ResolveSamplingParams("samp-level", "plain", "low", nil)
	if plain["temperature"] != 1 || plain["top_p"] != 0.9 {
		t.Fatalf("model without the field = %#v", plain)
	}
	clamped := ResolveSamplingParams("samp-level", "clamp", "off", nil)
	if clamped["temperature"] != 0.4 || clamped["top_p"] != 0.9 {
		t.Fatalf("clamped off = %#v", clamped)
	}
	ant := ResolveSamplingParams("samp-level", "ant", "off", map[string]any{"top_k": 0})
	if len(ant) != 1 || ant["top_k"] != 0 {
		t.Fatalf("anthropic api = %#v", ant)
	}
}

func TestOpenAICompletionsStreamSamplingByThinkingLevel(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-level-stream", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-level-stream", ID: "m", API: "openai-completions",
			SamplingParams: map[string]any{
				"temperature": 1,
				"top_p":       0.95,
				"top_k":       20,
			},
			SamplingParamsByThinkingLevel: map[string]map[string]any{
				"low":  {"temperature": 0.2},
				"high": {"temperature": 0.8, "top_p": 0.5},
			},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-level-stream") })

	var payloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"))
	}))
	defer srv.Close()

	client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	for _, level := range []string{"high", "low"} {
		stream, err := client.StreamFn()(context.Background(), Context{
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, Options{Provider: "samp-level-stream", Model: "m", Thinking: level, MaxTokens: 32})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		if final == nil || final.Text() != "ok" {
			t.Fatalf("final = %v", final)
		}
	}
	if len(payloads) != 2 {
		t.Fatalf("requests = %d", len(payloads))
	}
	high, low := payloads[0], payloads[1]
	if high["temperature"] != 0.8 || low["temperature"] != 0.2 {
		t.Fatalf("temperature high=%#v low=%#v", high["temperature"], low["temperature"])
	}
	if high["top_p"] != 0.5 || low["top_p"] != 0.95 {
		t.Fatalf("top_p high=%#v low=%#v", high["top_p"], low["top_p"])
	}
	if high["top_k"] != float64(20) || low["top_k"] != float64(20) {
		t.Fatalf("top_k high=%#v low=%#v", high["top_k"], low["top_k"])
	}
}
