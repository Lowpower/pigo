package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryCoercesStringNumbersAndBools(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	mustWrite(t, file, "one\ntwo\nthree\n")

	reg := NewBuiltins(Options{Cwd: dir})
	args := map[string]any{"path": "a.txt", "offset": "2"}
	out, isErr := reg.Execute(context.Background(), "read", args)
	if isErr {
		t.Fatalf("read offset string: %s", out)
	}
	if !strings.Contains(out, "two") || strings.HasPrefix(out, "one") {
		t.Fatalf("read offset string output = %q", out)
	}
	if args["offset"] != "2" {
		t.Fatalf("caller args mutated: %#v", args)
	}

	out, isErr = reg.Execute(context.Background(), "read", map[string]any{"path": "a.txt", "offset": "2.0"})
	if isErr || !strings.Contains(out, "two") {
		t.Fatalf("read offset 2.0 = %q isErr=%v", out, isErr)
	}

	out, isErr = reg.Execute(context.Background(), "bash", map[string]any{
		"command": "echo hi",
		"timeout": "5",
	})
	if isErr || !strings.Contains(out, "hi") {
		t.Fatalf("bash timeout string = %q isErr=%v", out, isErr)
	}
}

func TestRegistryRejectsFractionalInteger(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	mustWrite(t, file, "one\ntwo\n")
	reg := NewBuiltins(Options{Cwd: dir})

	for _, offset := range []any{2.5, "2.5"} {
		out, isErr := reg.Execute(context.Background(), "read", map[string]any{
			"path":   "a.txt",
			"offset": offset,
		})
		if !isErr {
			t.Fatalf("offset %v succeeded: %s", offset, out)
		}
		if !strings.Contains(out, "offset") || !strings.Contains(out, "integer") {
			t.Fatalf("offset %v error = %q", offset, out)
		}
		if strings.Contains(out, "readParams") || strings.Contains(out, "cannot unmarshal") {
			t.Fatalf("offset %v leaked decode error: %q", offset, out)
		}
	}
}

func TestRegistryNullOptionalMatchesOmitted(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	mustWrite(t, file, "one\ntwo\nthree\n")
	reg := NewBuiltins(Options{Cwd: dir})

	omitted, omittedErr := reg.Execute(context.Background(), "read", map[string]any{"path": "a.txt"})
	withNull, nullErr := reg.Execute(context.Background(), "read", map[string]any{"path": "a.txt", "limit": nil})
	if omittedErr || nullErr {
		t.Fatalf("omitted=%q err=%v null=%q err=%v", omitted, omittedErr, withNull, nullErr)
	}
	if omitted != withNull {
		t.Fatalf("null limit = %q, omitted = %q", withNull, omitted)
	}
}

func TestRegistryReportsMissingAndUnexpectedFields(t *testing.T) {
	reg := Default()

	out, isErr := reg.Execute(context.Background(), "read", map[string]any{})
	if !isErr || !strings.Contains(out, "path") || !strings.Contains(out, "required") {
		t.Fatalf("missing path = %q isErr=%v", out, isErr)
	}
	if strings.Contains(out, "readParams") {
		t.Fatalf("missing path leaked struct name: %q", out)
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "one\n")
	reg = NewBuiltins(Options{Cwd: dir})
	out, isErr = reg.Execute(context.Background(), "read", map[string]any{
		"path": "a.txt",
		"nope": true,
	})
	if !isErr || !strings.Contains(out, "unexpected field") || !strings.Contains(out, "nope") {
		t.Fatalf("extra field = %q isErr=%v", out, isErr)
	}
}

func TestRegistryValidatesExtensionTools(t *testing.T) {
	tool := &recordingTool{
		name: "ext",
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"count": map[string]any{"type": "integer"},
				"flag":  map[string]any{"type": "boolean"},
				"ratio": map[string]any{"type": "number"},
				"note": map[string]any{
					"anyOf": []any{
						map[string]any{"type": "number"},
						map[string]any{"type": "null"},
					},
				},
				"label": map[string]any{"type": []any{"string", "integer"}},
			},
			"required":             []any{"count"},
			"additionalProperties": false,
		},
	}
	reg := NewRegistry(tool)

	out, isErr := reg.Execute(context.Background(), "ext", map[string]any{
		"count": "3",
		"flag":  "true",
		"ratio": "1.5",
		"note":  nil,
		"label": "1",
	})
	if isErr {
		t.Fatalf("extension tool: %s", out)
	}
	if asInt(tool.got["count"]) != 3 {
		t.Fatalf("count = %#v", tool.got["count"])
	}
	if tool.got["flag"] != true {
		t.Fatalf("flag = %#v", tool.got["flag"])
	}
	if tool.got["ratio"] != 1.5 {
		t.Fatalf("ratio = %#v", tool.got["ratio"])
	}
	if note, ok := tool.got["note"]; !ok || note != nil {
		t.Fatalf("note = %#v present=%v, want null kept", tool.got["note"], ok)
	}
	if tool.got["label"] != "1" {
		t.Fatalf("label = %#v, want string kept", tool.got["label"])
	}

	out, isErr = reg.Execute(context.Background(), "ext", map[string]any{"flag": "true"})
	if !isErr || !strings.Contains(out, "count") || !strings.Contains(out, "required") {
		t.Fatalf("missing count = %q isErr=%v", out, isErr)
	}

	out, isErr = reg.Execute(context.Background(), "ext", map[string]any{"count": 2.5})
	if !isErr || !strings.Contains(out, "count") || !strings.Contains(out, "integer") {
		t.Fatalf("fractional count = %q isErr=%v", out, isErr)
	}

	out, isErr = reg.Execute(context.Background(), "ext", map[string]any{"count": 1, "extra": 1})
	if !isErr || !strings.Contains(out, "unexpected field") || !strings.Contains(out, "extra") {
		t.Fatalf("extra = %q isErr=%v", out, isErr)
	}
}

func TestRegistryEditNormalizesBeforeSchema(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	mustWrite(t, file, "one two\n")
	reg := NewRegistry(editTool{cwd: dir})

	out, isErr := reg.Execute(context.Background(), "edit", map[string]any{
		"path":  "a.txt",
		"edits": `[{"oldText":"one","newText":"ONE"}]`,
	})
	if isErr {
		t.Fatalf("string edits = %q", out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ONE two\n" {
		t.Fatalf("file after string edits = %q", data)
	}

	out, isErr = reg.Execute(context.Background(), "edit", map[string]any{
		"path":  "a.txt",
		"edits": map[string]any{"oldText": "two", "newText": "TWO"},
	})
	if isErr {
		t.Fatalf("single object edits = %q", out)
	}
	data, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ONE TWO\n" {
		t.Fatalf("file after single object = %q", data)
	}

	out, isErr = reg.Execute(context.Background(), "edit", map[string]any{
		"path":    "a.txt",
		"oldText": "ONE",
		"newText": "one",
	})
	if isErr {
		t.Fatalf("legacy edits = %q", out)
	}
	data, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one TWO\n" {
		t.Fatalf("file after legacy edits = %q", data)
	}
}

type recordingTool struct {
	name   string
	schema map[string]any
	got    map[string]any
}

func (t *recordingTool) Name() string           { return t.name }
func (t *recordingTool) Description() string    { return "record" }
func (t *recordingTool) Schema() map[string]any { return t.schema }
func (t *recordingTool) Execute(_ context.Context, args map[string]any) (string, bool) {
	t.got = args
	return "ok", false
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return -1
	}
}
