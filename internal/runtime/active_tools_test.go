package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

type fnTool struct {
	name   string
	desc   string
	schema map[string]any
	fn     func(ctx context.Context, args map[string]any) (string, bool)
}

func (t fnTool) Name() string           { return t.name }
func (t fnTool) Description() string    { return t.desc }
func (t fnTool) Schema() map[string]any { return t.schema }
func (t fnTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	if t.fn == nil {
		return "", false
	}
	return t.fn(ctx, args)
}

func objectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func TestSetActiveToolsIgnoresUnknownAndAdditiveDelta(t *testing.T) {
	e := &Engine{Tools: tools.NewRegistry(
		fnTool{name: "search", schema: objectSchema()},
		fnTool{name: "lookup", schema: objectSchema()},
		fnTool{name: "other", schema: objectSchema()},
	)}
	added := e.SetActiveTools([]string{"search", "ghost"})
	if added != nil {
		t.Fatalf("shrink from all is not additive, added=%v", added)
	}
	got := namesOf(e.providerTools())
	if fmt.Sprint(got) != "[search]" {
		t.Fatalf("active = %v", got)
	}

	added = e.SetActiveTools([]string{"search", "lookup", "ghost"})
	if fmt.Sprint(added) != "[lookup]" {
		t.Fatalf("additive added = %v", added)
	}
	got = namesOf(e.providerTools())
	if fmt.Sprint(got) != "[search lookup]" {
		t.Fatalf("active after add = %v", got)
	}

	added = e.SetActiveTools([]string{"lookup"})
	if added != nil {
		t.Fatalf("replace must not stamp, added=%v", added)
	}
	got = namesOf(e.providerTools())
	if fmt.Sprint(got) != "[lookup]" {
		t.Fatalf("replaced = %v", got)
	}
}

func TestExecutorStampsAddedToolsOnFireworksTurn(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		bodies = append(bodies, req)
		w.Header().Set("Content-Type", "text/event-stream")
		n := len(bodies)
		if n == 1 {
			_, _ = w.Write([]byte(searchThenStopSSE()))
			return
		}
		_, _ = w.Write([]byte(textDoneSSE()))
	}))
	t.Cleanup(srv.Close)

	e := &Engine{
		Provider: "fireworks",
		Opts:     Options{Config: config.Config{Provider: "fireworks", Model: "accounts/fireworks/models/deepseek-v4-flash-0731"}},
		Stream:   (&ai.AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}).StreamFn(),
	}
	e.Tools = tools.NewRegistry(
		fnTool{name: "tool_search", desc: "Find tools.", schema: objectSchema(), fn: func(context.Context, map[string]any) (string, bool) {
			e.SetActiveTools([]string{"tool_search", "lookup"})
			return "Found lookup.", false
		}},
		fnTool{name: "lookup", desc: "Look up a key.", schema: objectSchema()},
	)
	e.SetActiveTools([]string{"tool_search"})

	stream := e.RunPrompt(context.Background(), nil, "look up alpha", nil)
	events := stream.Collect()
	last := events[len(events)-1]
	if last.Type != "agent_end" {
		t.Fatalf("last = %s", last.Type)
	}
	var stamped []string
	for _, m := range last.Messages {
		if m.Role == "toolResult" {
			stamped = m.AddedToolNames
		}
	}
	if fmt.Sprint(stamped) != "[lookup]" {
		t.Fatalf("stamped = %v", stamped)
	}
	if len(bodies) < 2 {
		t.Fatalf("requests = %d", len(bodies))
	}
	second := bodies[1]
	toolsRaw, _ := second["tools"].([]any)
	if len(toolsRaw) != 2 {
		t.Fatalf("second tools = %#v", second["tools"])
	}
	first, _ := toolsRaw[0].(map[string]any)
	late, _ := toolsRaw[1].(map[string]any)
	if first["name"] != "tool_search" {
		t.Fatalf("prefix = %#v", first)
	}
	if late["name"] != "lookup" || late["defer_loading"] != true {
		t.Fatalf("deferred = %#v", late)
	}
	if !requestHasToolReference(second, "lookup") {
		t.Fatalf("missing tool_reference in %#v", second["messages"])
	}
}

func TestAdoptSessionRestoresActiveTools(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendActiveToolsChange([]string{"lookup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Tools: tools.NewRegistry(
		fnTool{name: "tool_search", schema: objectSchema()},
		fnTool{name: "lookup", schema: objectSchema()},
	)}
	e.AdoptSession(sess)
	got := namesOf(e.providerTools())
	if fmt.Sprint(got) != "[lookup]" {
		t.Fatalf("restored active = %v", got)
	}
}

func namesOf(tools []ai.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func requestHasToolReference(req map[string]any, name string) bool {
	msgs, _ := req["messages"].([]any)
	for _, raw := range msgs {
		m, _ := raw.(map[string]any)
		content, _ := m["content"].([]any)
		for _, c := range content {
			block, _ := c.(map[string]any)
			if block["type"] != "tool_result" {
				continue
			}
			inner, ok := block["content"].([]any)
			if !ok {
				continue
			}
			for _, item := range inner {
				ref, _ := item.(map[string]any)
				if ref["type"] == "tool_reference" && ref["tool_name"] == name {
					return true
				}
			}
		}
	}
	return false
}

func searchThenStopSSE() string {
	return strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"fw","usage":{"input_tokens":1,"output_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"search1","name":"tool_search","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"lookup\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
}

func textDoneSSE() string {
	return strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_2","model":"fw","usage":{"input_tokens":2,"output_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
}
