package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Lowpower/pigo/internal/ext"
)

func TestHelloExtension(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hello-ext")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	h, err := ext.Spawn(context.Background(), "hello", []string{bin}, ext.Options{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = h.Close() }()

	tools := h.Tools()
	if len(tools) != 1 || tools[0].Name != "hello" {
		t.Fatalf("tools = %+v, want one 'hello'", tools)
	}

	out, isErr := h.CallTool(context.Background(), "hello", map[string]any{"name": "pigo"})
	if isErr || out != "Hello, pigo!" {
		t.Fatalf("hello(pigo) = %q (isErr=%v), want Hello, pigo!", out, isErr)
	}

	out, isErr = h.CallTool(context.Background(), "hello", map[string]any{"name": 1})
	if !isErr {
		t.Fatalf("hello(non-string) = %q, want error", out)
	}
}
