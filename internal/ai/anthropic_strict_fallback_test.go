package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

const (
	grammarTooLarge = "The compiled grammar is too large, which would cause performance issues. Simplify your tool schemas or reduce the number of strict tools."
	tooManyStrict   = "Too many strict tools (32). The maximum number of strict tools supported is 20. Try reducing the number of tools marked as strict."
	tooManyUnions   = "Schemas contains too many parameters with union types (480 parameters with type arrays or anyOf). This causes exponential compilation cost. Reduce the number of nullable or union-typed parameters (limit: 16 parameters with unions)."
)

func TestAnthropicStrictToolFallbackRetriesPreferTools(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "strict-fb", map[string]*models.Compat{
		"on": {SupportsStrictTools: &on},
	})
	preferParams := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value":    map[string]any{"type": "string"},
			"optional": map[string]any{"type": "number"},
		},
		"required": []any{"value"},
	}
	requiredParams := map[string]any{
		"type":       "object",
		"properties": map[string]any{"q": map[string]any{"type": "string"}},
		"required":   []any{"q"},
	}
	ordinaryParams := map[string]any{
		"type":       "object",
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
		"required":   []any{"path"},
	}
	ctx := Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{
			{Name: "preferred", Description: "p", Parameters: preferParams, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"}},
			{Name: "required", Description: "r", Parameters: requiredParams, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "required"}},
			{Name: "ordinary", Description: "o", Parameters: ordinaryParams},
		},
	}
	opts := Options{Provider: "strict-fb", Model: "on"}

	for _, message := range []string{grammarTooLarge, tooManyStrict, tooManyUnions} {
		t.Run(message[:24], func(t *testing.T) {
			var bodies [][]byte
			var transforms int
			opts.TransformBody = func(body []byte) []byte {
				transforms++
				return body
			}
			final := runStrictFallback(t, ctx, opts, func(attempt int) (int, string) {
				if attempt == 1 {
					return http.StatusBadRequest, anthropicErrorBody(message, "invalid_request_error")
				}
				return http.StatusOK, anthropicEndTurnSSE
			}, &bodies)

			if transforms != 2 {
				t.Fatalf("TransformBody calls = %d, want 2", transforms)
			}
			if len(bodies) != 2 {
				t.Fatalf("requests = %d, want 2", len(bodies))
			}
			if final == nil || final.StopReason != StopStop {
				t.Fatalf("final = %#v, want stop", final)
			}
			assertStrictFallbackDiagnostic(t, final, message)

			first := toolsByName(t, bodies[0])
			second := toolsByName(t, bodies[1])
			if first["preferred"]["strict"] != true {
				t.Fatalf("first preferred = %#v", first["preferred"])
			}
			if _, ok := second["preferred"]["strict"]; ok {
				t.Fatalf("second preferred stayed strict: %#v", second["preferred"])
			}
			if got := mustJSON(t, second["preferred"]["input_schema"]); got != mustJSON(t, preferParams) {
				t.Fatalf("second preferred schema = %s, want %s", got, mustJSON(t, preferParams))
			}
			if !reflect.DeepEqual(second["required"], first["required"]) {
				t.Fatalf("required changed\nfirst %#v\nsecond %#v", first["required"], second["required"])
			}
			if second["required"]["strict"] != true {
				t.Fatalf("required strict = %#v", second["required"])
			}
			if !reflect.DeepEqual(second["ordinary"], first["ordinary"]) {
				t.Fatalf("ordinary changed\nfirst %#v\nsecond %#v", first["ordinary"], second["ordinary"])
			}
		})
	}
}

func TestAnthropicStrictToolFallbackSkipsUnrelatedErrors(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "strict-fb-skip", map[string]*models.Compat{
		"on": {SupportsStrictTools: &on},
	})
	ctx := Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{{
			Name: "preferred", Description: "p",
			Parameters:          map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}},
			ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"},
		}},
	}
	opts := Options{Provider: "strict-fb-skip", Model: "on"}
	cases := []struct {
		name, message, errType string
		status                 int
	}{
		{name: "unrelated 400", message: "Invalid tool schema", errType: "invalid_request_error", status: http.StatusBadRequest},
		{name: "wrong error type", message: grammarTooLarge, errType: "authentication_error", status: http.StatusBadRequest},
		{name: "wrong HTTP status", message: grammarTooLarge, errType: "invalid_request_error", status: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var bodies [][]byte
			final := runStrictFallback(t, ctx, opts, func(int) (int, string) {
				return tc.status, anthropicErrorBody(tc.message, tc.errType)
			}, &bodies)
			if len(bodies) != 1 {
				t.Fatalf("requests = %d, want 1", len(bodies))
			}
			if final == nil || final.StopReason != StopError || !strings.Contains(final.ErrorMessage, tc.message) {
				t.Fatalf("final = %#v", final)
			}
			if len(final.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", final.Diagnostics)
			}
		})
	}
}

func TestAnthropicStrictToolFallbackSkipsWithoutPreferStrict(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "strict-fb-none", map[string]*models.Compat{
		"on": {SupportsStrictTools: &on},
	})
	limited := map[string]any{
		"type":       "object",
		"properties": map[string]any{"n": map[string]any{"type": "integer", "minimum": 1}},
		"required":   []any{"n"},
	}
	plain := map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}
	cases := []struct {
		name  string
		tools []Tool
	}{
		{name: "required", tools: []Tool{{Name: "lookup", Description: "l", Parameters: plain, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "required"}}}},
		{name: "ordinary", tools: []Tool{{Name: "lookup", Description: "l", Parameters: plain}}},
		{name: "locally unsupported prefer", tools: []Tool{{Name: "lookup", Description: "l", Parameters: limited, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"}}}},
	}
	opts := Options{Provider: "strict-fb-none", Model: "on"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var bodies [][]byte
			final := runStrictFallback(t, Context{
				Messages: []Message{{Role: RoleUser, Content: "hi"}},
				Tools:    tc.tools,
			}, opts, func(int) (int, string) {
				return http.StatusBadRequest, anthropicErrorBody(grammarTooLarge, "invalid_request_error")
			}, &bodies)
			if len(bodies) != 1 {
				t.Fatalf("requests = %d, want 1", len(bodies))
			}
			if final == nil || final.StopReason != StopError || !strings.Contains(final.ErrorMessage, grammarTooLarge) {
				t.Fatalf("final = %#v", final)
			}
		})
	}
}

func TestAnthropicStrictToolFallbackSecondRejection(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "strict-fb-again", map[string]*models.Compat{
		"on": {SupportsStrictTools: &on},
	})
	second := "Required grammar is still too large"
	ctx := Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{
			{Name: "preferred", Description: "p", Parameters: map[string]any{"type": "object"}, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"}},
			{Name: "required", Description: "r", Parameters: map[string]any{"type": "object"}, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "required"}},
		},
	}
	var bodies [][]byte
	final := runStrictFallback(t, ctx, Options{Provider: "strict-fb-again", Model: "on"}, func(attempt int) (int, string) {
		msg := grammarTooLarge
		if attempt > 1 {
			msg = second
		}
		return http.StatusBadRequest, anthropicErrorBody(msg, "invalid_request_error")
	}, &bodies)
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	if final == nil || final.StopReason != StopError || !strings.Contains(final.ErrorMessage, second) {
		t.Fatalf("final = %#v", final)
	}
	if strings.Contains(final.ErrorMessage, grammarTooLarge) {
		t.Fatalf("error kept the first rejection: %s", final.ErrorMessage)
	}
	assertStrictFallbackDiagnostic(t, final, grammarTooLarge)
	first := toolsByName(t, bodies[0])
	secondTools := toolsByName(t, bodies[1])
	if _, ok := secondTools["preferred"]["strict"]; ok {
		t.Fatalf("second preferred stayed strict: %#v", secondTools["preferred"])
	}
	if secondTools["required"]["strict"] != true || !reflect.DeepEqual(secondTools["required"], first["required"]) {
		t.Fatalf("required = %#v, first %#v", secondTools["required"], first["required"])
	}
}

func TestAnthropicStrictToolFallbackNextRequestStaysStrict(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "strict-fb-next", map[string]*models.Compat{
		"on": {SupportsStrictTools: &on},
	})
	ctx := Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{{
			Name: "preferred", Description: "p",
			Parameters:          map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}},
			ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"},
		}},
	}
	opts := Options{Provider: "strict-fb-next", Model: "on"}
	var bodies [][]byte
	var attempt int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		bodies = append(bodies, body)
		if attempt == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(anthropicErrorBody(grammarTooLarge, "invalid_request_error")))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicEndTurnSSE))
	}))
	t.Cleanup(srv.Close)
	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	_, firstFinal := stream.Collect()
	if firstFinal == nil || firstFinal.StopReason != StopStop {
		t.Fatalf("first final = %#v", firstFinal)
	}
	stream, err = client.StreamFn()(context.Background(), ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if len(bodies) != 3 {
		t.Fatalf("requests = %d, want 3", len(bodies))
	}
	third := toolsByName(t, bodies[2])
	if third["preferred"]["strict"] != true {
		t.Fatalf("third request preferred = %#v", third["preferred"])
	}
	second := toolsByName(t, bodies[1])
	if _, ok := second["preferred"]["strict"]; ok {
		t.Fatalf("second request stayed strict: %#v", second["preferred"])
	}
}

func TestAnthropicStrictToolFallbackHonorsCancel(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "strict-fb-cancel", map[string]*models.Compat{
		"on": {SupportsStrictTools: &on},
	})
	ctx := Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{{
			Name: "preferred", Description: "p",
			Parameters:          map[string]any{"type": "object"},
			ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"},
		}},
	}
	callCtx, cancel := context.WithCancel(context.Background())
	var bodies int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bodies++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(anthropicErrorBody(grammarTooLarge, "invalid_request_error")))
	}))
	t.Cleanup(srv.Close)
	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	opts := Options{
		Provider: "strict-fb-cancel", Model: "on",
		OnHTTPResponse: func(status int, _ http.Header) {
			if status == http.StatusBadRequest {
				cancel()
			}
		},
	}
	_, err := client.StreamFn()(callCtx, ctx, opts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if bodies != 1 {
		t.Fatalf("requests = %d, want 1", bodies)
	}
}

func TestAnthropicStrictToolFallbackIgnoresSSEError(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "strict-fb-sse", map[string]*models.Compat{
		"on": {SupportsStrictTools: &on},
	})
	ctx := Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{{
			Name: "preferred", Description: "p",
			Parameters:          map[string]any{"type": "object"},
			ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"},
		}},
	}
	var bodies int
	sse := "event: error\ndata: " + anthropicErrorBody(grammarTooLarge, "invalid_request_error") + "\n\n"
	final := runStrictFallback(t, ctx, Options{Provider: "strict-fb-sse", Model: "on"}, func(int) (int, string) {
		bodies++
		return http.StatusOK, sse
	}, nil)
	if bodies != 1 {
		t.Fatalf("requests = %d, want 1", bodies)
	}
	if final == nil || final.StopReason != StopError || !strings.Contains(final.ErrorMessage, grammarTooLarge) {
		t.Fatalf("final = %#v", final)
	}
	if len(final.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", final.Diagnostics)
	}
}

const anthropicEndTurnSSE = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"on","usage":{"input_tokens":1,"output_tokens":0}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}
`

func anthropicErrorBody(message, errType string) string {
	body, err := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errType,
			"message": message,
		},
	})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func runStrictFallback(t *testing.T, reqCtx Context, opts Options, respond func(attempt int) (int, string), bodies *[][]byte) *AssistantMessage {
	t.Helper()
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if bodies != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			*bodies = append(*bodies, body)
		}
		status, payload := respond(n)
		if status != http.StatusOK {
			w.WriteHeader(status)
		} else {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), reqCtx, opts)
	if err != nil {
		t.Fatal(err)
	}
	events, final := stream.Collect()
	for _, ev := range events {
		if ev.Type == EventError && final != nil && final.StopReason == StopStop {
			t.Fatalf("success stream contained an error event: %#v", ev)
		}
	}
	return final
}

func assertStrictFallbackDiagnostic(t *testing.T, msg *AssistantMessage, firstMessage string) {
	t.Helper()
	if msg == nil || len(msg.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", msg)
	}
	got := msg.Diagnostics[0]
	if got.Type != "anthropic_strict_tools_fallback" || got.Timestamp == 0 || got.Details == nil || got.Error == nil {
		t.Fatalf("diagnostic = %#v", got)
	}
	if got.Details.Reason != "strict_tool_limits_exceeded" {
		t.Fatalf("reason = %q", got.Details.Reason)
	}
	if got.Error.Message != firstMessage {
		t.Fatalf("diagnostic message = %q, want %q", got.Error.Message, firstMessage)
	}
}
