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

const piMessagesFixture = `data: {"type":"start"}

data: {"type":"text_start","contentIndex":0}

data: {"type":"text_delta","contentIndex":0,"delta":"Hello"}

data: {"type":"text_end","contentIndex":0,"content":"Hello"}

data: {"type":"done","reason":"stop","usage":{"input":1,"output":1,"totalTokens":2}}

`

func TestPiMessagesClientHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "pi-qwen" {
			t.Errorf("model = %v", payload["model"])
		}
		ctx, _ := payload["context"].(map[string]any)
		if ctx["systemPrompt"] != "sys" {
			t.Errorf("context = %#v", ctx)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(piMessagesFixture))
	}))
	defer srv.Close()

	client := &PiMessagesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "pi-qwen"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello" {
		t.Fatalf("%+v", final)
	}
	if final.API != "pi-messages" || final.StopReason != StopStop {
		t.Fatalf("%+v", final)
	}
	if final.Usage.Input != 1 || final.Usage.Output != 1 {
		t.Fatalf("usage = %+v", final.Usage)
	}
}

func TestStreamForPiMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(piMessagesFixture))
	}))
	defer srv.Close()
	stream, err := StreamFor("radius", ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})(
		context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "pi-qwen"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello" {
		t.Fatalf("%+v", final)
	}
}

func TestPiMessagesRequiresBaseURL(t *testing.T) {
	stream, err := (&PiMessagesClient{APIKey: "k"}).StreamFn()(context.Background(), Context{}, Options{Model: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError || !strings.Contains(final.ErrorMessage, "base URL") {
		t.Fatalf("%+v", final)
	}
}
