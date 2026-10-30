package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKimiCodingDeferredToolsRequest(t *testing.T) {
	short, full := twoStepDeferredContext()
	for _, model := range []string{"k3", "k3-256k", "kimi-for-coding"} {
		t.Run(model, func(t *testing.T) {
			opts := Options{Provider: "kimi-coding", Model: model, MaxTokens: 16}
			shortReq := decodeReq(t, mustBuildOpenAI(t, short, opts))
			fullReq := decodeReq(t, mustBuildOpenAI(t, full, opts))
			if names := chatToolNames(t, shortReq["tools"]); len(names) != 1 || names[0] != "tool_search" {
				t.Fatalf("short tools = %v", names)
			}
			if names := chatToolNames(t, fullReq["tools"]); len(names) != 1 || names[0] != "tool_search" {
				t.Fatalf("full tools = %v", names)
			}
			shortMsgs, _ := shortReq["messages"].([]any)
			fullMsgs, _ := fullReq["messages"].([]any)
			if mustJSON(t, fullMsgs[:len(shortMsgs)]) != mustJSON(t, shortMsgs) {
				t.Fatalf("prefix changed\nshort %s\nfull %s", mustJSON(t, shortMsgs), mustJSON(t, fullMsgs[:len(shortMsgs)]))
			}
			batches := kimiToolBatches(t, fullMsgs)
			if len(batches) != 2 || batches[0][0] != "lookup" || batches[1][0] != "extra" {
				t.Fatalf("batches = %#v", batches)
			}
			if _, ok := fullReq["store"]; ok {
				t.Fatalf("store = %#v", fullReq["store"])
			}
			if fullReq["max_tokens"] != float64(16) {
				t.Fatalf("max_tokens = %#v", fullReq["max_tokens"])
			}
			if _, ok := fullReq["max_completion_tokens"]; ok {
				t.Fatalf("max_completion_tokens = %#v", fullReq["max_completion_tokens"])
			}
			if role := shortMsgs[0].(map[string]any)["role"]; role != "user" {
				t.Fatalf("first role = %#v", role)
			}
		})
	}
}

func TestKimiCodingHighspeedKeepsTopLevelTools(t *testing.T) {
	_, full := twoStepDeferredContext()
	req := decodeReq(t, mustBuildOpenAI(t, full, Options{Provider: "kimi-coding", Model: "kimi-for-coding-highspeed"}))
	if names := chatToolNames(t, req["tools"]); len(names) != 3 {
		t.Fatalf("tools = %v", names)
	}
	if batches := kimiToolBatches(t, req["messages"].([]any)); len(batches) != 0 {
		t.Fatalf("batches = %#v", batches)
	}
}

func TestKimiCodingK3ReasoningContent(t *testing.T) {
	ctx := Context{System: "be brief", Messages: []Message{{
		Role: RoleAssistant,
		Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindText, Text: "done"},
		}},
	}}}
	for _, model := range []string{"k3", "kimi-for-coding"} {
		req := decodeReq(t, mustBuildOpenAI(t, ctx, Options{Provider: "kimi-coding", Model: model}))
		msgs, _ := req["messages"].([]any)
		if msgs[0].(map[string]any)["role"] != "system" {
			t.Fatalf("%s system role = %#v", model, msgs[0])
		}
		asst := msgs[1].(map[string]any)
		if asst["reasoning_content"] != "" {
			t.Fatalf("%s reasoning_content = %#v", model, asst["reasoning_content"])
		}
	}
}

func TestKimiCodingChatCompletionsURL(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	post := func(provider, base string) {
		t.Helper()
		client := &OpenAICompletionsClient{BaseURL: base, APIKey: "k", HTTPClient: srv.Client()}
		stream, err := client.StreamFn()(context.Background(), Context{
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, Options{Provider: provider, Model: "kimi-for-coding"})
		if err != nil {
			t.Fatal(err)
		}
		stream.Collect()
	}

	post("kimi-coding", srv.URL+"/coding/v1")
	post("kimi-coding", srv.URL+"/coding")
	post("deepseek", srv.URL)
	if len(paths) != 3 {
		t.Fatalf("paths = %v", paths)
	}
	if paths[0] != "/coding/v1/chat/completions" {
		t.Fatalf("builtin-style path = %q", paths[0])
	}
	if paths[1] != "/coding/chat/completions" {
		t.Fatalf("missing /v1 path = %q", paths[1])
	}
	if paths[2] != "/v1/chat/completions" {
		t.Fatalf("deepseek path = %q", paths[2])
	}
}

func TestKimiCodingFakeServerAcceptsAddedTool(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "path", http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	short, full := twoStepDeferredContext()
	client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	opts := Options{Provider: "kimi-coding", Model: "kimi-for-coding"}
	for _, ctx := range []Context{short, full} {
		stream, err := client.StreamFn()(context.Background(), ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		if final == nil || final.StopReason == StopError {
			t.Fatalf("stream = %#v", final)
		}
	}
	if len(bodies) != 2 {
		t.Fatalf("bodies = %d", len(bodies))
	}
	if names := chatToolNames(t, bodies[0]["tools"]); len(names) != 1 || names[0] != "tool_search" {
		t.Fatalf("first tools = %v", names)
	}
	if names := chatToolNames(t, bodies[1]["tools"]); len(names) != 1 || names[0] != "tool_search" {
		t.Fatalf("second tools = %v", names)
	}
	if mustJSON(t, bodies[0]["tools"]) != mustJSON(t, bodies[1]["tools"]) {
		t.Fatalf("prefix tools replaced\nfirst %s\nsecond %s", mustJSON(t, bodies[0]["tools"]), mustJSON(t, bodies[1]["tools"]))
	}
	batches := kimiToolBatches(t, bodies[1]["messages"].([]any))
	if len(batches) != 2 || batches[0][0] != "lookup" || batches[1][0] != "extra" {
		t.Fatalf("batches = %#v", batches)
	}
}
