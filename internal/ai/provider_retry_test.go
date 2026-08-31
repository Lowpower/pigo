package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWrapProviderRetryRetriesSetupError(t *testing.T) {
	var n int32
	inner := StreamFn(func(ctx context.Context, _ Context, _ Options) (*EventStream, error) {
		i := atomic.AddInt32(&n, 1)
		if i == 1 {
			return nil, errors.New("429 too many requests")
		}
		return EmitMessage(ctx, &AssistantMessage{Role: RoleAssistant, StopReason: StopStop}), nil
	})
	fn := WrapProviderRetry(inner, ProviderRetry{MaxRetries: 2, MaxDelay: time.Millisecond})
	stream, err := fn(context.Background(), Context{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for range stream.Events() {
		count++
	}
	if count == 0 {
		t.Fatal("expected stream events")
	}
	if atomic.LoadInt32(&n) != 2 {
		t.Fatalf("calls=%d", n)
	}
}

func TestWrapProviderRetryDoesNotRetryOverflow(t *testing.T) {
	var n int32
	inner := StreamFn(func(_ context.Context, _ Context, _ Options) (*EventStream, error) {
		atomic.AddInt32(&n, 1)
		return nil, errors.New("prompt is too long")
	})
	fn := WrapProviderRetry(inner, ProviderRetry{MaxRetries: 3, MaxDelay: time.Millisecond})
	_, err := fn(context.Background(), Context{}, Options{})
	if err == nil {
		t.Fatal("expected error")
	}
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("calls=%d", n)
	}
}

func TestWrapProviderRetryAppliesTimeout(t *testing.T) {
	inner := StreamFn(func(ctx context.Context, _ Context, _ Options) (*EventStream, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	fn := WrapProviderRetry(inner, ProviderRetry{Timeout: 20 * time.Millisecond, MaxRetries: 0})
	start := time.Now()
	_, err := fn(context.Background(), Context{}, Options{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("timeout too slow")
	}
}
