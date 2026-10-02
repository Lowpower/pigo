package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
)

func TestModelFromMapSamplingParams(t *testing.T) {
	got := modelFromMap("local", map[string]any{
		"id": "m",
		"samplingParams": map[string]any{
			"top_k": 0,
			"min_p": 0.1,
		},
	})
	if got.ID != "m" || got.SamplingParams["top_k"] != 0 || got.SamplingParams["min_p"] != 0.1 {
		t.Fatalf("%+v", got)
	}
	empty := modelFromMap("local", map[string]any{
		"id":             "e",
		"samplingParams": map[string]any{},
	})
	if empty.ID != "e" || len(empty.SamplingParams) != 0 {
		t.Fatalf("empty = %#v", empty.SamplingParams)
	}
}

func TestHostModelStreamAndCompleteShareSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-host", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-host", ID: "m", API: "openai-completions",
			SamplingParams: map[string]any{
				"temperature": 1,
				"top_p":       0.9,
				"top_k":       0,
			},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-host") })

	var got []ai.Options
	e := &Engine{
		Provider: "samp-host",
		Opts:     Options{Config: config.Config{Provider: "samp-host", Model: "m"}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			got = append(got, opts)
			return ai.ScriptedStreamFn("ok", 0)(ctx, req, opts)
		},
	}
	if res := e.HandleHostCall(nil, "model.stream", nil); res["ok"] != true {
		t.Fatalf("stream = %#v", res)
	}
	if res := e.HandleHostCall(nil, "model.complete", map[string]any{
		"samplingParams": map[string]any{"temperature": 0},
	}); res["message"] == nil {
		t.Fatalf("complete = %#v", res)
	}
	if len(got) != 2 {
		t.Fatalf("calls = %d", len(got))
	}
	if got[0].SamplingParams["temperature"] != 1 || got[0].SamplingParams["top_p"] != 0.9 || got[0].SamplingParams["top_k"] != 0 {
		t.Fatalf("stream params = %#v", got[0].SamplingParams)
	}
	if got[1].SamplingParams["temperature"] != 0 || got[1].SamplingParams["top_p"] != 0.9 || got[1].SamplingParams["top_k"] != 0 {
		t.Fatalf("complete params = %#v", got[1].SamplingParams)
	}
	if got[0].Provider != "samp-host" || got[0].Model != "m" || got[1].Provider != got[0].Provider || got[1].Model != got[0].Model {
		t.Fatalf("targets = %#v %#v", got[0], got[1])
	}
}

func TestHostModelCompleteSendsSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-http", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-http", ID: "m", API: "openai-completions",
			SamplingParams: map[string]any{
				"temperature": 1,
				"top_p":       0.55,
				"top_k":       0,
			},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-http") })

	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"))
	}))
	defer srv.Close()

	e := &Engine{
		Provider: "samp-http",
		Opts:     Options{Config: config.Config{Provider: "samp-http", Model: "m"}},
		Stream: (&ai.OpenAICompletionsClient{
			BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(),
		}).StreamFn(),
	}
	res := e.HandleHostCall(nil, "model.complete", map[string]any{
		"messages":       []any{map[string]any{"role": "user", "content": "hi"}},
		"samplingParams": map[string]any{"temperature": 0},
	})
	if res["error"] != nil {
		t.Fatal(res["error"])
	}
	msg, _ := res["message"].(map[string]any)
	if msg == nil {
		t.Fatalf("complete = %#v", res)
	}
	if payload["temperature"] != float64(0) {
		t.Fatalf("temperature = %#v", payload["temperature"])
	}
	if payload["top_p"] != 0.55 {
		t.Fatalf("top_p = %#v", payload["top_p"])
	}
	if payload["top_k"] != float64(0) {
		t.Fatalf("top_k = %#v", payload["top_k"])
	}
}
