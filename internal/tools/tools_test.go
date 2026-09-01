package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/image/bmp"
)

func run(t *testing.T, tool Tool, args map[string]any) (string, bool) {
	t.Helper()
	return tool.Execute(context.Background(), args)
}

func TestReadImageReturnsContentBlocks(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dot.png")
	data, err := decodeTestPNG()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}

	out, isErr := run(t, readTool{}, map[string]any{"path": file})
	if isErr {
		t.Fatalf("read image error: %s", out)
	}
	if !strings.Contains(out, `"type":"image"`) || !strings.Contains(out, `"mimeType":"image/png"`) {
		t.Fatalf("read image should return image content JSON, got %q", out)
	}
	if !strings.Contains(out, "Read image file") {
		t.Fatalf("read image missing text note: %q", out)
	}
}

func TestReadBMPConvertsToPNG(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dot.bmp")
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := bmp.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	out, isErr := run(t, readTool{}, map[string]any{"path": file})
	if isErr {
		t.Fatalf("read bmp error: %s", out)
	}
	if !strings.Contains(out, `"mimeType":"image/png"`) {
		t.Fatalf("BMP should convert to PNG, got %q", out)
	}
	if !strings.Contains(out, "converted from image/bmp") {
		t.Fatalf("missing conversion hint: %q", out)
	}
}

func TestReadResizesOversizeImage(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "wide.png")
	img := image.NewRGBA(image.Rect(0, 0, 2001, 10))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	out, isErr := run(t, readTool{autoResize: true}, map[string]any{"path": file})
	if isErr {
		t.Fatalf("read oversize error: %s", out)
	}
	if !strings.Contains(out, "resized from 2001x10") {
		t.Fatalf("expected resize hint, got %q", out)
	}
}

func TestReadSkipsResizeWhenAutoResizeOff(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "wide.png")
	img := image.NewRGBA(image.Rect(0, 0, 2001, 10))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out, isErr := run(t, readTool{autoResize: false}, map[string]any{"path": file})
	if isErr {
		t.Fatalf("read oversize error: %s", out)
	}
	if strings.Contains(out, "resized from") {
		t.Fatalf("should not resize: %q", out)
	}
}

func decodeTestPNG() ([]byte, error) {
	return base64.StdEncoding.DecodeString(testPNG1x1)
}

const testPNG1x1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestWriteReadEdit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")

	if out, isErr := run(t, writeTool{}, map[string]any{"path": file, "content": "hello\nworld\n"}); isErr {
		t.Fatalf("write failed: %s", out)
	}

	out, isErr := run(t, readTool{}, map[string]any{"path": file})
	if isErr || !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Fatalf("read = %q (isErr=%v)", out, isErr)
	}

	// read with offset/limit (line 2 only)
	out, isErr = run(t, readTool{}, map[string]any{"path": file, "offset": float64(2), "limit": float64(1)})
	if isErr || strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Fatalf("read offset/limit = %q", out)
	}

	// edit: replace "world" -> "gophers"
	out, isErr = run(t, editTool{}, map[string]any{
		"path":  file,
		"edits": []any{map[string]any{"oldText": "world", "newText": "gophers"}},
	})
	if isErr || !strings.Contains(out, "Successfully replaced 1 block") {
		t.Fatalf("edit = %q (isErr=%v)", out, isErr)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "gophers") {
		t.Fatalf("file after edit = %q", string(data))
	}
}

func TestEditRejectsNonUnique(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dup.txt")
	_ = os.WriteFile(file, []byte("x x x"), 0o644)

	out, isErr := run(t, editTool{}, map[string]any{
		"path":  file,
		"edits": []any{map[string]any{"oldText": "x", "newText": "y"}},
	})
	if !isErr || !strings.Contains(out, "not unique") {
		t.Fatalf("expected non-unique error, got %q (isErr=%v)", out, isErr)
	}
}

func TestListFindGrep(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\nfunc Foo() {}\n")
	mustWrite(t, filepath.Join(dir, "b.txt"), "hello foo\n")
	mustWrite(t, filepath.Join(dir, "sub", "c.go"), "package sub\n// foo bar\n")

	// ls
	out, isErr := run(t, listTool{}, map[string]any{"path": dir})
	if isErr || !strings.Contains(out, "a.go") || !strings.Contains(out, "sub/") {
		t.Fatalf("ls = %q", out)
	}

	// find **/*.go -> a.go and sub/c.go
	out, isErr = run(t, findTool{}, map[string]any{"path": dir, "pattern": "**/*.go"})
	if isErr || !strings.Contains(out, "a.go") || !strings.Contains(out, "sub/c.go") {
		t.Fatalf("find = %q", out)
	}
	// top-level *.go must not match nested
	out, _ = run(t, findTool{}, map[string]any{"path": dir, "pattern": "*.go"})
	if strings.Contains(out, "sub/c.go") {
		t.Fatalf("*.go should not match nested file, got %q", out)
	}

	// grep "foo" across all files
	out, isErr = run(t, grepTool{}, map[string]any{"path": dir, "pattern": "foo", "ignoreCase": true})
	if isErr || !strings.Contains(out, "a.go:2:") || !strings.Contains(out, "b.txt:1:") {
		t.Fatalf("grep = %q", out)
	}
	// grep with glob filter to .go only
	out, _ = run(t, grepTool{}, map[string]any{"path": dir, "pattern": "foo", "ignoreCase": true, "glob": "**/*.go"})
	if strings.Contains(out, "b.txt") {
		t.Fatalf("grep glob filter leaked non-go file: %q", out)
	}
}

func TestGrepFindHonorGitignore(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	mustWrite(t, filepath.Join(dir, ".gitignore"), "secret.txt\nbuild/\n*.tmp\n")
	mustWrite(t, filepath.Join(dir, "visible.txt"), "needle-visible\n")
	mustWrite(t, filepath.Join(dir, "secret.txt"), "needle-secret\n")
	mustWrite(t, filepath.Join(dir, "build", "out.go"), "needle-build\n")
	mustWrite(t, filepath.Join(dir, "scratch.tmp"), "needle-tmp\n")
	mustWrite(t, filepath.Join(dir, "src", "ok.go"), "needle-ok\n")

	out, isErr := run(t, grepTool{}, map[string]any{"path": dir, "pattern": "needle"})
	if isErr {
		t.Fatalf("grep error: %s", out)
	}
	if !strings.Contains(out, "visible.txt") || !strings.Contains(out, "src/ok.go") {
		t.Fatalf("grep missed unignored files: %q", out)
	}
	if strings.Contains(out, "secret.txt") || strings.Contains(out, "build/") || strings.Contains(out, "scratch.tmp") {
		t.Fatalf("grep leaked gitignored files: %q", out)
	}

	out, isErr = run(t, findTool{}, map[string]any{"path": dir, "pattern": "**/*"})
	if isErr {
		t.Fatalf("find error: %s", out)
	}
	if !strings.Contains(out, "visible.txt") || !strings.Contains(out, "src/ok.go") {
		t.Fatalf("find missed unignored files: %q", out)
	}
	if strings.Contains(out, "secret.txt") || strings.Contains(out, "build/") || strings.Contains(out, "scratch.tmp") {
		t.Fatalf("find leaked gitignored files: %q", out)
	}
}

func TestGrepOutsideGitIgnoresGitignore(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "secret.txt\n")
	mustWrite(t, filepath.Join(dir, "secret.txt"), "needle-secret\n")

	out, isErr := run(t, grepTool{}, map[string]any{"path": dir, "pattern": "needle"})
	if isErr || !strings.Contains(out, "secret.txt") {
		t.Fatalf("grep outside git should search gitignored files (rg default), got %q (isErr=%v)", out, isErr)
	}
}

func TestFindOutsideGitHonorsGitignore(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "secret.txt\n")
	mustWrite(t, filepath.Join(dir, "secret.txt"), "x\n")
	mustWrite(t, filepath.Join(dir, "visible.txt"), "x\n")

	out, isErr := run(t, findTool{}, map[string]any{"path": dir, "pattern": "*.txt"})
	if isErr {
		t.Fatalf("find error: %s", out)
	}
	if !strings.Contains(out, "visible.txt") {
		t.Fatalf("find missed visible.txt: %q", out)
	}
	if strings.Contains(out, "secret.txt") {
		t.Fatalf("find outside git should still honor .gitignore (fd --no-require-git), got %q", out)
	}
}

func TestFindNestedRepoStopsParentGitignore(t *testing.T) {
	parent := t.TempDir()
	gitInit(t, parent)
	mustWrite(t, filepath.Join(parent, ".gitignore"), "hidden.txt\n")
	mustWrite(t, filepath.Join(parent, "hidden.txt"), "parent-hidden\n")

	nested := filepath.Join(parent, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, nested)
	mustWrite(t, filepath.Join(nested, "hidden.txt"), "nested-visible\n")
	mustWrite(t, filepath.Join(nested, "ok.txt"), "ok\n")

	out, isErr := run(t, findTool{}, map[string]any{"path": nested, "pattern": "*.txt"})
	if isErr {
		t.Fatalf("find nested error: %s", out)
	}
	if !strings.Contains(out, "hidden.txt") || !strings.Contains(out, "ok.txt") {
		t.Fatalf("nested repo should not inherit parent gitignore, got %q", out)
	}

	out, isErr = run(t, findTool{}, map[string]any{"path": parent, "pattern": "**/*.txt"})
	if isErr {
		t.Fatalf("find parent error: %s", out)
	}
	if containsLine(out, "hidden.txt") {
		t.Fatalf("parent hidden.txt should stay ignored, got %q", out)
	}
	if !strings.Contains(out, "nested/hidden.txt") {
		t.Fatalf("parent search should still see nested repo file, got %q", out)
	}
}

func TestGrepExplicitPathReadsIgnoredFile(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	mustWrite(t, filepath.Join(dir, ".gitignore"), "secret.txt\n")
	secret := filepath.Join(dir, "secret.txt")
	mustWrite(t, secret, "needle-secret\n")

	out, isErr := run(t, grepTool{}, map[string]any{"path": secret, "pattern": "needle"})
	if isErr || !strings.Contains(out, "needle-secret") {
		t.Fatalf("grep on an explicit file should read it even if gitignored, got %q (isErr=%v)", out, isErr)
	}
}

func TestGrepGitignoreNegation(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	mustWrite(t, filepath.Join(dir, ".gitignore"), "*.log\n!keep.log\n")
	mustWrite(t, filepath.Join(dir, "noise.log"), "needle-noise\n")
	mustWrite(t, filepath.Join(dir, "keep.log"), "needle-keep\n")

	out, isErr := run(t, grepTool{}, map[string]any{"path": dir, "pattern": "needle"})
	if isErr {
		t.Fatalf("grep error: %s", out)
	}
	if !strings.Contains(out, "keep.log") {
		t.Fatalf("negated gitignore should re-include keep.log: %q", out)
	}
	if strings.Contains(out, "noise.log") {
		t.Fatalf("grep leaked ignored *.log: %q", out)
	}
}

func TestGrepGitInfoExclude(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	mustWrite(t, filepath.Join(dir, ".git", "info", "exclude"), "local-only.txt\n")
	mustWrite(t, filepath.Join(dir, "local-only.txt"), "needle-local\n")
	mustWrite(t, filepath.Join(dir, "ok.txt"), "needle-ok\n")

	out, isErr := run(t, grepTool{}, map[string]any{"path": dir, "pattern": "needle"})
	if isErr {
		t.Fatalf("grep error: %s", out)
	}
	if !strings.Contains(out, "ok.txt") {
		t.Fatalf("grep missed ok.txt: %q", out)
	}
	if strings.Contains(out, "local-only.txt") {
		t.Fatalf("grep leaked .git/info/exclude file: %q", out)
	}
}

func TestGrepNestedGitignoreIsRelative(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	mustWrite(t, filepath.Join(dir, "src", ".gitignore"), "/hidden.txt\n")
	mustWrite(t, filepath.Join(dir, "src", "hidden.txt"), "needle-src\n")
	mustWrite(t, filepath.Join(dir, "hidden.txt"), "needle-root\n")

	out, isErr := run(t, grepTool{}, map[string]any{"path": dir, "pattern": "needle"})
	if isErr {
		t.Fatalf("grep error: %s", out)
	}
	if !strings.Contains(out, "hidden.txt:1:needle-root") {
		t.Fatalf("root hidden.txt should not be ignored by src/.gitignore: %q", out)
	}
	if strings.Contains(out, "src/hidden.txt") {
		t.Fatalf("src/.gitignore /hidden.txt should ignore src/hidden.txt: %q", out)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func containsLine(out, name string) bool {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == name {
			return true
		}
	}
	return false
}

func TestBash(t *testing.T) {
	out, isErr := run(t, bashTool{}, map[string]any{"command": "echo hello-bash"})
	if isErr || !strings.Contains(out, "hello-bash") {
		t.Fatalf("bash echo = %q (isErr=%v)", out, isErr)
	}
	out, isErr = run(t, bashTool{}, map[string]any{"command": "exit 3"})
	if !isErr || !strings.Contains(out, "exit code 3") {
		t.Fatalf("bash exit = %q (isErr=%v)", out, isErr)
	}
}

func TestBashTruncatesLongOutput(t *testing.T) {
	out, isErr := run(t, bashTool{}, map[string]any{
		"command": "seq 1 2100",
	})
	if isErr {
		t.Fatalf("seq error: %s", out)
	}
	if !strings.Contains(out, "Full output:") {
		t.Fatalf("expected truncation footer, got %q", out[max(0, len(out)-300):])
	}
	if strings.HasPrefix(strings.TrimSpace(out), "1\n") {
		t.Fatalf("should keep the tail, not the head: %q", out[:80])
	}
	if !strings.Contains(out, "2100") {
		t.Fatalf("missing last line: %q", out[max(0, len(out)-200):])
	}
	if i := strings.LastIndex(out, "Full output: "); i >= 0 {
		path := strings.TrimSuffix(out[i+len("Full output: "):], "]")
		t.Cleanup(func() { _ = os.Remove(path) })
	}
}

func TestSchemasAndRegistry(t *testing.T) {
	reg := Default()
	names := map[string]bool{}
	for _, tl := range reg.AITools() {
		names[tl.Name] = true
		if tl.Parameters["type"] != "object" {
			t.Errorf("%s schema type = %v, want object", tl.Name, tl.Parameters["type"])
		}
		if tl.Description == "" {
			t.Errorf("%s has empty description", tl.Name)
		}
	}
	for _, want := range []string{"read", "write", "edit", "bash", "grep", "find", "ls"} {
		if !names[want] {
			t.Errorf("registry missing tool %q", want)
		}
	}
	if runtime.GOOS != "windows" && names["powershell"] {
		t.Errorf("powershell should be Windows-only")
	}
	if runtime.GOOS == "windows" && !names["powershell"] {
		t.Errorf("windows registry missing powershell")
	}

	// read's schema must mark path required.
	var readSchema map[string]any
	for _, tl := range reg.AITools() {
		if tl.Name == "read" {
			readSchema = tl.Parameters
		}
	}
	req, _ := readSchema["required"].([]any)
	found := false
	for _, r := range req {
		if r == "path" {
			found = true
		}
	}
	if !found {
		t.Errorf("read schema required = %v, want it to contain path", req)
	}

	// dispatch by name
	out, isErr := reg.Execute(context.Background(), "nope", nil)
	if !isErr || !strings.Contains(out, "unknown tool") {
		t.Errorf("unknown tool dispatch = %q", out)
	}
}

// TestToolsShowcase runs every tool against a temp workspace and logs the real
// outputs (serves as living documentation and demo evidence under -v).
func TestToolsShowcase(t *testing.T) {
	dir := t.TempDir()
	reg := Default()
	show := func(name string, args map[string]any) {
		out, isErr := reg.Execute(context.Background(), name, args)
		t.Logf("\n$ tool %s %v  (isError=%v)\n%s", name, args, isErr, out)
	}

	show("write", map[string]any{"path": filepath.Join(dir, "greeting.txt"), "content": "hello\nworld\n"})
	show("write", map[string]any{"path": filepath.Join(dir, "src/app.go"), "content": "package app\n\nfunc Greet() string { return \"hi\" }\n"})
	show("ls", map[string]any{"path": dir})
	show("find", map[string]any{"path": dir, "pattern": "**/*.go"})
	show("grep", map[string]any{"path": dir, "pattern": "func", "context": 0})
	show("read", map[string]any{"path": filepath.Join(dir, "greeting.txt")})
	show("edit", map[string]any{
		"path":  filepath.Join(dir, "greeting.txt"),
		"edits": []any{map[string]any{"oldText": "world", "newText": "gophers"}},
	})
	show("bash", map[string]any{"command": "echo tools-work && uname -s"})
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
