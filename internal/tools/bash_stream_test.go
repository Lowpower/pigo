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
