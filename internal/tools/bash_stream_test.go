package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestBashStreamsAccumulatedOutput(t *testing.T) {
	var mu sync.Mutex
	var snaps []string
	ctx := WithOutputUpdate(context.Background(), func(accumulated string) {
		mu.Lock()
		snaps = append(snaps, accumulated)
		mu.Unlock()
	})
	out, isErr := bashTool{}.Execute(ctx, map[string]any{"command": "printf hello-stream"})
	if isErr || !strings.Contains(out, "hello-stream") {
		t.Fatalf("result = %q isErr=%v", out, isErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(snaps) == 0 {
		t.Fatal("expected streaming updates during bash execution")
	}
	if !strings.Contains(snaps[len(snaps)-1], "hello-stream") {
		t.Fatalf("snapshots = %#v", snaps)
	}
}

func TestBashPrependsCommandPrefix(t *testing.T) {
	out, isErr := bashTool{prefix: "TEST_PIGO_PREFIX=1"}.Execute(context.Background(), map[string]any{
		"command": "printf %s \"$TEST_PIGO_PREFIX\"",
	})
	if isErr || !strings.Contains(out, "1") {
		t.Fatalf("prefixed bash = %q isErr=%v", out, isErr)
	}
}
