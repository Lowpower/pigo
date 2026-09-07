package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolvePath(t *testing.T) {
	if got := resolvePath("", ""); got != "." {
		t.Fatalf("empty = %q", got)
	}
	if got := resolvePath("/tmp/work", ""); got != "/tmp/work" {
		t.Fatalf("cwd fallback = %q", got)
	}
	if got := resolvePath("/tmp/work", "rel.txt"); got != filepath.Join("/tmp/work", "rel.txt") {
		t.Fatalf("join = %q", got)
	}
	abs := "/abs/file"
	if runtime.GOOS == "windows" {
		abs = `C:\abs\file`
	}
	if got := resolvePath("/tmp/work", abs); got != abs {
		t.Fatalf("abs = %q", got)
	}
}

func TestCwdResolvesRelativeToolPaths(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "note.txt"), "hello cwd\n")
	reg := NewBuiltins(Options{Cwd: dir})
	out, isErr := reg.Execute(t.Context(), "read", map[string]any{"path": "note.txt"})
	if isErr || out != "hello cwd\n" {
		t.Fatalf("read relative = %q isErr=%v", out, isErr)
	}
	out, isErr = reg.Execute(t.Context(), "ls", map[string]any{})
	if isErr || !strings.Contains(out, "note.txt") {
		t.Fatalf("ls cwd = %q isErr=%v", out, isErr)
	}
}

func TestBashInjectsExtraEnvAndCwd(t *testing.T) {
	dir := t.TempDir()
	tool := bashTool{cwd: dir, env: map[string]string{"PIGO_SESSION_ID": "sess-1", "AI_AGENT": "pigo"}}
	out, isErr := tool.Execute(t.Context(), map[string]any{
		"command": "printf '%s %s %s' \"$PIGO_SESSION_ID\" \"$AI_AGENT\" \"$(basename \"$PWD\")\"",
	})
	if isErr {
		t.Fatalf("bash error: %s", out)
	}
	if !strings.Contains(out, "sess-1") || !strings.Contains(out, "pigo") || !strings.Contains(out, filepath.Base(dir)) {
		t.Fatalf("env/cwd = %q", out)
	}
}

func TestEditAcceptsLegacyAndSingleObject(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	mustWrite(t, file, "alpha beta\n")
	out, isErr := run(t, editTool{}, map[string]any{
		"path":    file,
		"oldText": "alpha",
		"newText": "ALPHA",
	})
	if isErr || !strings.Contains(out, "Successfully replaced 1 block") {
		t.Fatalf("legacy edit = %q isErr=%v", out, isErr)
	}
	mustWrite(t, file, "alpha beta\n")
	out, isErr = run(t, editTool{}, map[string]any{
		"path":  file,
		"edits": map[string]any{"oldText": "beta", "newText": "BETA"},
	})
	if isErr {
		t.Fatalf("single object edit = %q", out)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "alpha BETA\n" {
		t.Fatalf("file = %q", data)
	}
	mustWrite(t, file, "one two\n")
	out, isErr = run(t, editTool{}, map[string]any{
		"path":  file,
		"edits": `[{"oldText":"one","newText":"ONE"}]`,
	})
	if isErr {
		t.Fatalf("json-string edits = %q", out)
	}
}

func TestConstrainedSamplingOnBuiltins(t *testing.T) {
	reg := Default()
	want := map[string]bool{"read": true, "bash": true, "edit": true, "write": true, "grep": false, "find": false, "ls": false}
	for _, tl := range reg.AITools() {
		expect := want[tl.Name]
		got := tl.ConstrainedSampling != nil
		if got != expect {
			t.Errorf("%s constrained = %v want %v", tl.Name, got, expect)
		}
		if got && (tl.ConstrainedSampling.Type != "json_schema" || tl.ConstrainedSampling.Strict != "prefer") {
			t.Errorf("%s sampling = %+v", tl.Name, tl.ConstrainedSampling)
		}
	}
}
