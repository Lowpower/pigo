package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepFallsBackWithoutRipgrep(t *testing.T) {
	orig := lookRipgrep
	lookRipgrep = func() (string, error) { return "", errors.New("missing") }
	t.Cleanup(func() { lookRipgrep = orig })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, isErr := grepTool{}.Execute(context.Background(), map[string]any{
		"path": dir, "pattern": "needle",
	})
	if isErr || !strings.Contains(out, "a.txt:1:hello needle") {
		t.Fatalf("fallback grep = %q isErr=%v", out, isErr)
	}
}

func TestGrepRipgrepWhenAvailable(t *testing.T) {
	if _, err := lookRipgrep(); err != nil {
		t.Skip("rg not on PATH")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, isErr := grepTool{}.Execute(context.Background(), map[string]any{
		"path": dir, "pattern": "needle",
	})
	if isErr || !strings.Contains(out, "a.txt:1:hello needle") {
		t.Fatalf("rg grep = %q isErr=%v", out, isErr)
	}
}
