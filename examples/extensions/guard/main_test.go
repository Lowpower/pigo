package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ext"
)

func TestGuardExtension(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "guard-ext")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	h, err := ext.Spawn(context.Background(), "guard", []string{bin}, ext.Options{
		UnknownFlags: []ext.UnknownFlag{{Name: "plan", Present: true}},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = h.Close() }()

	if !h.HasShortcut("ctrl+shift+g") {
		t.Fatal("missing shortcut ctrl+shift+g")
	}
	v, ok := h.FlagValue("plan")
	if !ok || v != true {
		t.Fatalf("plan flag = %v %v, want true", v, ok)
	}

	res, err := h.QueryEvent(context.Background(), "input", map[string]any{"text": "summarize this"})
	if err != nil {
		t.Fatal(err)
	}
	if res["action"] != "transform" {
		t.Fatalf("action = %v, want transform", res["action"])
	}
	text, _ := res["text"].(string)
	if !strings.Contains(text, "Plan first") || !strings.Contains(text, "summarize this") {
		t.Fatalf("transformed text = %q", text)
	}
}

func TestGuardExtensionOff(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "guard-ext")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	h, err := ext.Spawn(context.Background(), "guard", []string{bin}, ext.Options{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = h.Close() }()

	res, err := h.QueryEvent(context.Background(), "input", map[string]any{"text": "summarize this"})
	if err != nil {
		t.Fatal(err)
	}
	if res["action"] != nil && res["action"] != "" {
		t.Fatalf("expected no transform, got %+v", res)
	}
}
