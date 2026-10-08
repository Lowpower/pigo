package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{59 * time.Second, "59.0s"},
		{65 * time.Second, "1m 5s"},
		{time.Hour + 2*time.Minute, "1h 2m 0s"},
	}
	for _, tc := range cases {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestAppendDuration(t *testing.T) {
	got := appendDuration("hello\n[exit code 3]", 65*time.Second)
	if !strings.HasSuffix(got, "\nTook 1m 5s") {
		t.Fatalf("appendDuration = %q", got)
	}
	if !strings.Contains(got, "[exit code 3]") {
		t.Fatalf("status line dropped: %q", got)
	}
}

func TestBashResultIncludesDuration(t *testing.T) {
	out, isErr := bashTool{}.Execute(context.Background(), map[string]any{"command": "echo hello-bash"})
	if isErr || !strings.Contains(out, "hello-bash") {
		t.Fatalf("bash = %q isErr=%v", out, isErr)
	}
	if !strings.Contains(out, "\nTook ") {
		t.Fatalf("missing duration: %q", out)
	}
}

func TestBashTimeoutKeepsElapsed(t *testing.T) {
	out, isErr := bashTool{}.Execute(context.Background(), map[string]any{
		"command": "sleep 30",
		"timeout": 1,
	})
	if !isErr || !strings.Contains(out, "[timed out after 1s]") {
		t.Fatalf("timeout = %q isErr=%v", out, isErr)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "s") || !strings.Contains(out, "\nTook ") {
		t.Fatalf("timeout missing elapsed: %q", out)
	}
}

func TestBashCancelKeepsElapsed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, isErr := bashTool{}.Execute(ctx, map[string]any{"command": "sleep 30"})
	if !isErr {
		t.Fatalf("cancel should fail: %q", out)
	}
	if !strings.Contains(out, "\nTook ") {
		t.Fatalf("cancel missing elapsed: %q", out)
	}
}

func TestBashStreamOmitsDurationUntilDone(t *testing.T) {
	var mu sync.Mutex
	var snaps []string
	ctx := WithOutputUpdate(context.Background(), func(accumulated string) {
		mu.Lock()
		snaps = append(snaps, accumulated)
		mu.Unlock()
	})
	out, isErr := bashTool{}.Execute(ctx, map[string]any{"command": "printf hello-stream"})
	if isErr || !strings.Contains(out, "hello-stream") || !strings.Contains(out, "\nTook ") {
		t.Fatalf("result = %q isErr=%v", out, isErr)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, snap := range snaps {
		if strings.Contains(snap, "Took ") {
			t.Fatalf("partial update included duration: %q", snap)
		}
	}
}
