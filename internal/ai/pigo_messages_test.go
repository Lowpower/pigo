package ai

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

const pigoMessagesFixture = `data: {"type":"start"}

data: {"type":"text_start","contentIndex":0}

data: {"type":"text_delta","contentIndex":0,"delta":"Hello"}

data: {"type":"text_end","contentIndex":0,"content":"Hello"}

data: {"type":"done","reason":"stop","usage":{"input":1,"output":1,"totalTokens":2}}

`

func TestPigoMessagesClientHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "qwen" {
			t.Errorf("model = %v", payload["model"])
		}
		ctx, _ := payload["context"].(map[string]any)
		if ctx["systemPrompt"] != "sys" {
			t.Errorf("context = %#v", ctx)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(pigoMessagesFixture))
	}))
	defer srv.Close()

	client := &PigoMessagesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "qwen"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello" {
		t.Fatalf("%+v", final)
	}
	if final.API != "pigo-messages" || final.StopReason != StopStop {
		t.Fatalf("%+v", final)
	}
	if final.Usage.Input != 1 || final.Usage.Output != 1 {
		t.Fatalf("usage = %+v", final.Usage)
	}
}

func TestStreamForPigoMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(pigoMessagesFixture))
	}))
	defer srv.Close()
	stream, err := StreamFor("radius", ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})(
		context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "qwen"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello" {
		t.Fatalf("%+v", final)
	}
}

func TestPigoMessagesRequiresBaseURL(t *testing.T) {
	stream, err := (&PigoMessagesClient{APIKey: "k"}).StreamFn()(context.Background(), Context{}, Options{Model: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError || !strings.Contains(final.ErrorMessage, "base URL") {
		t.Fatalf("%+v", final)
	}
}

func TestPigoMessagesCatalogCost(t *testing.T) {
	const provider = "cost-msgs"
	models.RegisterProvider(models.ProviderSpec{
		ID: provider, DefaultAPI: "pigo-messages", DefaultID: "priced",
		Models: []models.Model{
			{Provider: provider, ID: "priced", API: "pigo-messages", Cost: &models.Cost{Input: 3, Output: 15, CacheRead: 0.30, CacheWrite: 3.75}},
			{Provider: provider, ID: "free", API: "pigo-messages"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider(provider) })

	run := func(model, usage string) Usage {
		t.Helper()
		body := "data: {\"type\":\"done\",\"reason\":\"stop\",\"usage\":" + usage + "}\n\n"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, body)
		}))
		defer srv.Close()
		client := &PigoMessagesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
		stream, err := client.StreamFn()(context.Background(), Context{
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, Options{Provider: provider, Model: model})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		if final == nil {
			t.Fatal("no final message")
		}
		return final.Usage
	}

	u := run("priced", `{"input":1000000,"output":1000000,"totalTokens":2000000,"cost":{"input":1,"output":1,"cacheRead":1,"cacheWrite":1,"total":4}}`)
	if math.Abs(u.Cost.Input-3) > 1e-9 || math.Abs(u.Cost.Output-15) > 1e-9 || math.Abs(u.Cost.Total-18) > 1e-9 {
		t.Fatalf("priced cost = %+v, want total 18", u.Cost)
	}

	u = run("priced", `{"input":300000,"output":0,"cacheRead":500000,"cacheWrite":200000,"totalTokens":1000000}`)
	if math.Abs(u.Cost.Input-0.9) > 1e-9 || math.Abs(u.Cost.CacheRead-0.15) > 1e-9 || math.Abs(u.Cost.CacheWrite-0.75) > 1e-9 || math.Abs(u.Cost.Total-1.8) > 1e-9 {
		t.Fatalf("cache cost = %+v", u.Cost)
	}

	u = run("free", `{"input":1000000,"output":1000000,"totalTokens":2000000,"cost":{"input":9,"output":9,"cacheRead":9,"cacheWrite":9,"total":36}}`)
	if u.Cost != (UsageCost{}) {
		t.Fatalf("free cost = %+v, want zeros", u.Cost)
	}
}
