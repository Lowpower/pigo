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

func TestStrictToolParametersReadBashEdit(t *testing.T) {
	read := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Path to the file to read"},
			"offset": map[string]any{"type": "integer", "description": "Line number to start reading from"},
			"limit":  map[string]any{"type": "integer", "description": "Maximum number of lines to read"},
		},
		"required":             []any{"path"},
		"additionalProperties": false,
	}
	bash := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string"},
			"timeout": map[string]any{"type": "integer"},
		},
		"required":             []any{"command"},
		"additionalProperties": false,
	}
	edit := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
			"edits": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"oldText": map[string]any{"type": "string"},
						"newText": map[string]any{"type": "string"},
					},
					"required": []any{"oldText", "newText"},
				},
			},
		},
		"required": []any{"path", "edits"},
	}

	gotRead := strictToolParameters(read)
	assertRequired(t, gotRead, "limit", "offset", "path")
	assertType(t, prop(t, gotRead, "path"), "string")
	assertTypeList(t, prop(t, gotRead, "offset"), "integer", "null")
	assertTypeList(t, prop(t, gotRead, "limit"), "integer", "null")
	if prop(t, gotRead, "offset")["description"] != "Line number to start reading from" {
		t.Fatalf("offset description = %#v", prop(t, gotRead, "offset")["description"])
	}
	if gotRead["additionalProperties"] != false {
		t.Fatalf("read additionalProperties = %#v", gotRead["additionalProperties"])
	}

	gotBash := strictToolParameters(bash)
	assertRequired(t, gotBash, "command", "timeout")
	assertType(t, prop(t, gotBash, "command"), "string")
	assertTypeList(t, prop(t, gotBash, "timeout"), "integer", "null")

	gotEdit := strictToolParameters(edit)
	assertRequired(t, gotEdit, "edits", "path")
	assertType(t, prop(t, gotEdit, "path"), "string")
	assertType(t, prop(t, gotEdit, "edits"), "array")
	items, _ := prop(t, gotEdit, "edits")["items"].(map[string]any)
	if items == nil {
		t.Fatal("edits.items missing")
	}
	assertRequired(t, items, "newText", "oldText")
	assertType(t, prop(t, items, "oldText"), "string")
	assertType(t, prop(t, items, "newText"), "string")
	if items["additionalProperties"] != false {
		t.Fatalf("edits.items additionalProperties = %#v", items["additionalProperties"])
	}

	for _, schema := range []map[string]any{read, bash, edit} {
		if _, ok := schema["properties"].(map[string]any)["offset"].(map[string]any); ok {
			if schema["properties"].(map[string]any)["offset"].(map[string]any)["type"] != "integer" {
				t.Fatal("read offset type was mutated")
			}
		}
	}
	if req, _ := read["required"].([]any); len(req) != 1 || req[0] != "path" {
		t.Fatalf("read required mutated: %#v", read["required"])
	}
	if _, ok := edit["properties"].(map[string]any)["edits"].(map[string]any)["items"].(map[string]any)["additionalProperties"]; ok {
		t.Fatal("edit items additionalProperties was written onto the original schema")
	}
}

func TestStrictToolParametersNullableShapes(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"choice": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
				},
			},
			"label": map[string]any{"type": []any{"string", "integer"}},
			"note": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "null"},
				},
			},
			"flag": map[string]any{"type": "boolean"},
			"mode": map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
				},
			},
		},
		"required": []any{"flag"},
	}
	got := strictToolParameters(schema)
	choice := prop(t, got, "choice")
	anyOf, _ := choice["anyOf"].([]any)
	if len(anyOf) != 3 || choice["type"] != nil {
		t.Fatalf("choice = %#v", choice)
	}
	nullBranch, _ := anyOf[2].(map[string]any)
	if nullBranch["type"] != "null" {
		t.Fatalf("choice null branch = %#v", anyOf)
	}
	assertTypeList(t, prop(t, got, "label"), "string", "integer", "null")
	note := prop(t, got, "note")
	noteAny, _ := note["anyOf"].([]any)
	if len(noteAny) != 2 {
		t.Fatalf("note anyOf grew: %#v", note)
	}
	assertType(t, prop(t, got, "flag"), "boolean")
	mode := prop(t, got, "mode")
	oneOf, _ := mode["oneOf"].([]any)
	if len(oneOf) != 2 || mode["type"] != nil {
		t.Fatalf("mode = %#v", mode)
	}
	modeNull, _ := oneOf[1].(map[string]any)
	if modeNull["type"] != "null" {
		t.Fatalf("mode null branch = %#v", oneOf)
	}
}

func TestStrictToolParametersLeavesOriginalUntouched(t *testing.T) {
	nested := map[string]any{"type": "integer"}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"timeout": nested,
		},
		"required": []any{"path"},
	}
	before := mustJSON(t, schema)
	_ = strictToolParameters(schema)
	if got := mustJSON(t, schema); got != before {
		t.Fatalf("schema changed\nbefore %s\nafter  %s", before, got)
	}
	if nested["type"] != "integer" {
		t.Fatalf("nested type = %#v", nested["type"])
	}
}

func TestChatCompletionsStrictSchemaConverted(t *testing.T) {
	on := true
	models.RegisterProvider(models.ProviderSpec{
		ID: "strict-schema", DefaultAPI: "openai-completions", DefaultID: "plain",
		Models: []models.Model{
			{Provider: "strict-schema", ID: "plain"},
			{Provider: "strict-schema", ID: "strict", Compat: &models.Compat{SupportsStrictMode: &on}},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("strict-schema") })

	read := readLikeSchema()
	grep := map[string]any{
		"type":       "object",
		"properties": map[string]any{"pattern": map[string]any{"type": "string"}},
		"required":   []any{"pattern"},
	}
	beforeRead := mustJSON(t, read)
	beforeGrep := mustJSON(t, grep)
	ctx := Context{Tools: []Tool{
		{Name: "read", Parameters: read, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"}},
		{Name: "grep", Parameters: grep},
	}}

	plain := chatToolFunctions(t, ctx, Options{Provider: "strict-schema", Model: "plain"})
	if _, ok := plain["read"]["strict"]; ok {
		t.Fatalf("plain strict = %#v", plain["read"]["strict"])
	}
	plainRead, _ := plain["read"]["parameters"].(map[string]any)
	assertType(t, prop(t, plainRead, "offset"), "integer")
	if plainRead["additionalProperties"] != false {
		t.Fatalf("plain read should keep the original schema: %#v", plainRead)
	}
	plainGrep, _ := plain["grep"]["parameters"].(map[string]any)
	if _, ok := plainGrep["additionalProperties"]; ok {
		t.Fatalf("plain grep = %#v", plainGrep)
	}

	strict := chatToolFunctions(t, ctx, Options{Provider: "strict-schema", Model: "strict"})
	if strict["read"]["strict"] != true {
		t.Fatalf("strict flag = %#v", strict["read"]["strict"])
	}
	strictRead, _ := strict["read"]["parameters"].(map[string]any)
	assertRequired(t, strictRead, "limit", "offset", "path")
	assertTypeList(t, prop(t, strictRead, "offset"), "integer", "null")
	assertType(t, prop(t, strictRead, "path"), "string")
	if _, ok := strict["grep"]["strict"]; ok {
		t.Fatalf("non-strict tool gained strict: %#v", strict["grep"])
	}
	strictGrep, _ := strict["grep"]["parameters"].(map[string]any)
	if _, ok := strictGrep["additionalProperties"]; ok {
		t.Fatalf("non-strict tool schema changed: %#v", strictGrep)
	}

	cerebras := chatToolFunctions(t, ctx, Options{Provider: "cerebras", Model: "strict"})
	if _, ok := cerebras["read"]["strict"]; ok {
		t.Fatalf("cerebras strict = %#v", cerebras["read"]["strict"])
	}
	cerebrasRead, _ := cerebras["read"]["parameters"].(map[string]any)
	assertType(t, prop(t, cerebrasRead, "offset"), "integer")

	if got := mustJSON(t, read); got != beforeRead {
		t.Fatalf("read parameters mutated\n%s", got)
	}
	if got := mustJSON(t, grep); got != beforeGrep {
		t.Fatalf("grep parameters mutated\n%s", got)
	}
}

func TestOpenAIResponsesStrictToolSchema(t *testing.T) {
	read := readLikeSchema()
	grep := map[string]any{
		"type":       "object",
		"properties": map[string]any{"pattern": map[string]any{"type": "string"}},
		"required":   []any{"pattern"},
	}
	beforeRead := mustJSON(t, read)
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("body json: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesFixture))
	}))
	t.Cleanup(srv.Close)

	client := &OpenAIResponsesClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{
			{Name: "read", Parameters: read, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"}},
			{Name: "grep", Description: "search", Parameters: grep},
		},
	}, Options{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()

	tools := responseTools(t, payload)
	readTool := tools["read"]
	if readTool["strict"] != true {
		t.Fatalf("read strict = %#v", readTool["strict"])
	}
	readParams, _ := readTool["parameters"].(map[string]any)
	assertRequired(t, readParams, "limit", "offset", "path")
	assertTypeList(t, prop(t, readParams, "limit"), "integer", "null")
	assertType(t, prop(t, readParams, "path"), "string")
	if readParams["additionalProperties"] != false {
		t.Fatalf("read additionalProperties = %#v", readParams["additionalProperties"])
	}

	grepTool := tools["grep"]
	grepParams, _ := grepTool["parameters"].(map[string]any)
	if _, ok := grepParams["additionalProperties"]; ok {
		t.Fatalf("grep schema = %#v", grepParams)
	}
	assertType(t, prop(t, grepParams, "pattern"), "string")
	if grepTool["strict"] == true {
		t.Fatalf("grep strict = %#v", grepTool["strict"])
	}
	if got := mustJSON(t, read); got != beforeRead {
		t.Fatalf("read parameters mutated\n%s", got)
	}
}

func readLikeSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string"},
			"offset": map[string]any{"type": "integer"},
			"limit":  map[string]any{"type": "integer"},
		},
		"required":             []any{"path"},
		"additionalProperties": false,
	}
}

func chatToolFunctions(t *testing.T, ctx Context, opts Options) map[string]map[string]any {
	t.Helper()
	body, err := buildOpenAIRequest(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	tools, _ := req["tools"].([]any)
	out := map[string]map[string]any{}
	for _, raw := range tools {
		fn, _ := raw.(map[string]any)["function"].(map[string]any)
		name, _ := fn["name"].(string)
		out[name] = fn
	}
	return out
}

func responseTools(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()
	tools, _ := payload["tools"].([]any)
	out := map[string]map[string]any{}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		if name == "" {
			if fn, ok := tool["function"].(map[string]any); ok {
				tool = fn
				name, _ = fn["name"].(string)
			}
		}
		out[name] = tool
	}
	if out["read"] == nil || out["grep"] == nil {
		t.Fatalf("tools = %#v", payload["tools"])
	}
	return out
}

func prop(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	p, _ := props[name].(map[string]any)
	if p == nil {
		t.Fatalf("missing property %s in %#v", name, schema)
	}
	return p
}

func assertRequired(t *testing.T, schema map[string]any, want ...string) {
	t.Helper()
	req, _ := schema["required"].([]any)
	if len(req) != len(want) {
		t.Fatalf("required = %#v, want %v", req, want)
	}
	for i, key := range want {
		if req[i] != key {
			t.Fatalf("required = %#v, want %v", req, want)
		}
	}
}

func assertType(t *testing.T, schema map[string]any, want string) {
	t.Helper()
	if schema["type"] != want {
		t.Fatalf("type = %#v, want %q", schema["type"], want)
	}
}

func assertTypeList(t *testing.T, schema map[string]any, want ...string) {
	t.Helper()
	got, _ := schema["type"].([]any)
	if len(got) != len(want) {
		t.Fatalf("type = %#v, want %v", schema["type"], want)
	}
	for i, typ := range want {
		if got[i] != typ {
			t.Fatalf("type = %#v, want %v", schema["type"], want)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
