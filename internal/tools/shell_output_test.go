package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWallSecondsUsesTenths(t *testing.T) {
	if wallSeconds(1400*time.Millisecond) != 1.4 {
		t.Fatalf("1400ms = %v", wallSeconds(1400*time.Millisecond))
	}
	if wallSeconds(1450*time.Millisecond) != 1.5 {
		t.Fatalf("1450ms = %v", wallSeconds(1450*time.Millisecond))
	}
	if wallSeconds(-time.Second) != 0 {
		t.Fatalf("negative = %v", wallSeconds(-time.Second))
	}
}

func TestBashOutputSchemaReachesAITools(t *testing.T) {
	var schema map[string]any
	for _, tool := range Default().AITools() {
		if tool.Name == "bash" {
			schema = tool.OutputSchema
		}
	}
	if schema["type"] != "object" {
		t.Fatalf("schema = %#v", schema)
	}
	props, _ := schema["properties"].(map[string]any)
	for _, name := range []string{"output", "truncated", "full_output_path", "exit_code", "wall_time_seconds"} {
		if props[name] == nil {
			t.Fatalf("missing %s", name)
		}
	}
}

func TestBashStructuredEmptyAndExit(t *testing.T) {
	empty := runShell(t, "true")
	if empty.isErr || empty.model == "" || !strings.HasPrefix(empty.model, "\nTook ") {
		t.Fatalf("model = %q err %v", empty.model, empty.isErr)
	}
	if strings.Contains(empty.model, "(no output)") {
		t.Fatalf("model text changed: %q", empty.model)
	}
	if empty.body.Output != "" || empty.body.Truncated || empty.body.ExitCode != 0 || empty.body.FullOutputPath != "" {
		t.Fatalf("empty = %+v", empty.body)
	}
	if _, ok := empty.raw["full_output_path"]; ok {
		t.Fatalf("empty result included full_output_path: %s", empty.structured)
	}

	failed := runShell(t, "printf bye; exit 3")
	if !failed.isErr || !strings.Contains(failed.model, "bye") || !strings.Contains(failed.model, "[exit code 3]") || !strings.Contains(failed.model, "\nTook ") {
		t.Fatalf("model = %q err %v", failed.model, failed.isErr)
	}
	if failed.body.Output != "bye" || failed.body.Truncated || failed.body.ExitCode != 3 || failed.body.FullOutputPath != "" {
		t.Fatalf("exit = %+v", failed.body)
	}
}

func TestBashStructuredMidSizeKeepsModelTruncation(t *testing.T) {
	dir := t.TempDir()
	full := strings.Repeat("x", 60*1024)
	if err := os.WriteFile(filepath.Join(dir, "mid.bin"), []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	got := runShellAt(t, dir, "cat mid.bin")
	if got.isErr {
		t.Fatalf("model error: %s", got.model)
	}
	if !strings.Contains(got.model, "Full output:") || !strings.Contains(got.model, "\nTook ") {
		t.Fatalf("model footer missing: %s", got.model[len(got.model)-180:])
	}
	if len(got.model) >= len(full) {
		t.Fatalf("model text len %d is not truncated", len(got.model))
	}
	if got.body.Truncated || got.body.Output != full || got.body.FullOutputPath != "" || got.body.ExitCode != 0 {
		t.Fatalf("structured truncated=%v path=%q exit=%d out=%d", got.body.Truncated, got.body.FullOutputPath, got.body.ExitCode, len(got.body.Output))
	}
	if _, ok := got.raw["full_output_path"]; ok {
		t.Fatal("mid-size result included full_output_path")
	}
}

func TestBashStructuredTwoMiB(t *testing.T) {
	dir := t.TempDir()
	const headMark = "HEAD"
	const tailMark = "TAIL"
	full := headMark + strings.Repeat("M", 2*1024*1024-len(headMark)-len(tailMark)) + tailMark
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	got := runShellAt(t, dir, "cat big.bin")
	if got.isErr {
		t.Fatalf("model error: %s", tailOf(got.model))
	}
	if !strings.Contains(got.model, "Full output:") || !strings.Contains(got.model, "\nTook ") || strings.Contains(got.model, headMark) {
		t.Fatalf("model text = %s", tailOf(got.model))
	}
	if len(got.model) > DefaultMaxBytes+512 {
		t.Fatalf("model text len %d exceeds the visible cap", len(got.model))
	}
	if !got.body.Truncated || got.body.ExitCode != 0 {
		t.Fatalf("structured = %+v", got.body)
	}
	marker := "\n\n[... 1048576 bytes omitted ...]\n\n"
	if !strings.HasPrefix(got.body.Output, headMark) || !strings.HasSuffix(got.body.Output, tailMark) || !strings.Contains(got.body.Output, marker) {
		t.Fatalf("structured output missing head, tail, or marker")
	}
	info, err := os.Stat(got.body.FullOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(got.body.FullOutputPath) })
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	saved, err := os.ReadFile(got.body.FullOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != full {
		t.Fatalf("temp file len %d", len(saved))
	}
	if !strings.Contains(got.model, got.body.FullOutputPath) {
		t.Fatal("model footer and structured path differ")
	}
}

func TestBashStructuredSkipsTimeoutAndCancel(t *testing.T) {
	sink := &StructuredSink{}
	ctx := WithStructuredSink(context.Background(), sink)
	out, isErr := bashTool{}.Execute(ctx, map[string]any{"command": "sleep 30", "timeout": 1})
	if !isErr || !strings.Contains(out, "[timed out after 1s]") || !strings.Contains(out, "\nTook ") {
		t.Fatalf("timeout = %q err %v", out, isErr)
	}
	if len(sink.Raw) != 0 {
		t.Fatalf("timeout stored structured %s", sink.Raw)
	}

	sink = &StructuredSink{}
	cancelled, cancel := context.WithCancel(WithStructuredSink(context.Background(), sink))
	cancel()
	out, isErr = bashTool{}.Execute(cancelled, map[string]any{"command": "sleep 30"})
	if !isErr || !strings.Contains(out, "\nTook ") {
		t.Fatalf("cancel = %q err %v", out, isErr)
	}
	if len(sink.Raw) != 0 {
		t.Fatalf("cancel stored structured %s", sink.Raw)
	}
}

type shellRun struct {
	model      string
	isErr      bool
	structured []byte
	raw        map[string]any
	body       shellStructured
}

func runShell(t *testing.T, command string) shellRun {
	t.Helper()
	return runShellAt(t, "", command)
}

func runShellAt(t *testing.T, cwd, command string) shellRun {
	t.Helper()
	sink := &StructuredSink{}
	ctx := WithStructuredSink(context.Background(), sink)
	out, isErr := bashTool{cwd: cwd}.Execute(ctx, map[string]any{"command": command})
	got := shellRun{model: out, isErr: isErr, structured: append([]byte(nil), sink.Raw...)}
	if len(sink.Raw) == 0 {
		t.Fatalf("missing structured result for %q model %q", command, tailOf(out))
	}
	if err := json.Unmarshal(sink.Raw, &got.raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sink.Raw, &got.body); err != nil {
		t.Fatal(err)
	}
	return got
}

func tailOf(s string) string {
	if len(s) <= 240 {
		return s
	}
	return s[len(s)-240:]
}
