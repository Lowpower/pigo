package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestWrapProviderRetryRetriesStreamError(t *testing.T) {
	var n int32
	inner := StreamFn(func(ctx context.Context, _ Context, _ Options) (*EventStream, error) {
		i := atomic.AddInt32(&n, 1)
		if i == 1 {
			return EmitMessage(ctx, &AssistantMessage{
				Role: RoleAssistant, StopReason: StopError, ErrorMessage: "429 too many requests",
			}), nil
		}
		return EmitMessage(ctx, &AssistantMessage{
			Role: RoleAssistant, StopReason: StopStop,
			Content: []*Content{{Type: KindText, Text: "ok"}},
		}), nil
	})
	fn := WrapProviderRetry(inner, ProviderRetry{MaxRetries: 2, MaxDelay: time.Millisecond})
	stream, err := fn(context.Background(), Context{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopStop {
		t.Fatalf("final=%+v", final)
	}
	if atomic.LoadInt32(&n) != 2 {
		t.Fatalf("calls=%d", n)
	}
}

func TestWrapProviderRetryDoesNotRetryStreamOverflow(t *testing.T) {
	var n int32
	inner := StreamFn(func(ctx context.Context, _ Context, _ Options) (*EventStream, error) {
		atomic.AddInt32(&n, 1)
		return EmitMessage(ctx, &AssistantMessage{
			Role: RoleAssistant, StopReason: StopError, ErrorMessage: "prompt is too long",
		}), nil
	})
	fn := WrapProviderRetry(inner, ProviderRetry{MaxRetries: 3, MaxDelay: time.Millisecond})
	stream, err := fn(context.Background(), Context{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError {
		t.Fatalf("final=%+v", final)
	}
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("calls=%d", n)
	}
}

func TestWrapProviderRetryDoesNotRetryAfterText(t *testing.T) {
	var n int32
	inner := StreamFn(func(ctx context.Context, _ Context, _ Options) (*EventStream, error) {
		atomic.AddInt32(&n, 1)
		return EmitMessage(ctx, &AssistantMessage{
			Role: RoleAssistant, StopReason: StopError, ErrorMessage: "429 too many requests",
			Content: []*Content{{Type: KindText, Text: "partial"}},
		}), nil
	})
	fn := WrapProviderRetry(inner, ProviderRetry{MaxRetries: 3, MaxDelay: time.Millisecond})
	stream, err := fn(context.Background(), Context{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError {
		t.Fatalf("final=%+v", final)
	}
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("calls=%d", n)
	}
}

func TestProviderRetryDelay(t *testing.T) {
	now := time.Date(2026, 10, 21, 7, 0, 0, 0, time.UTC)
	maxDelay := 60 * time.Second

	both := http.Header{}
	both.Set("Retry-After-Ms", "1500")
	both.Set("Retry-After", "9")
	delay, err := providerRetryDelay(both, 0, maxDelay, now, "boom")
	if err != nil || delay != 1500*time.Millisecond {
		t.Fatalf("retry-after-ms precedence: delay=%s err=%v", delay, err)
	}

	seconds := http.Header{}
	seconds.Set("Retry-After", "2")
	delay, err = providerRetryDelay(seconds, 0, maxDelay, now, "boom")
	if err != nil || delay != 2*time.Second {
		t.Fatalf("retry-after seconds: delay=%s err=%v", delay, err)
	}

	dated := http.Header{}
	dated.Set("Retry-After", now.Add(2*time.Second).UTC().Format(http.TimeFormat))
	delay, err = providerRetryDelay(dated, 0, maxDelay, now, "boom")
	if err != nil || delay != 2*time.Second {
		t.Fatalf("retry-after date: delay=%s err=%v", delay, err)
	}

	bad := http.Header{}
	bad.Set("Retry-After", "not-a-date")
	delay, err = providerRetryDelay(bad, 0, maxDelay, now, "boom")
	if err != nil {
		t.Fatal(err)
	}
	if delay < 375*time.Millisecond || delay > 500*time.Millisecond {
		t.Fatalf("unparseable date backoff=%s", delay)
	}

	tooLong := http.Header{}
	tooLong.Set("Retry-After", "120")
	_, err = providerRetryDelay(tooLong, 0, maxDelay, now, "openai API error 429: rate limit")
	const want = "Server requested 120s retry delay (max: 60s). openai API error 429: rate limit"
	if err == nil || err.Error() != want {
		t.Fatalf("err=%v", err)
	}

	delay, err = providerRetryDelay(tooLong, 0, 0, now, "boom")
	if err != nil || delay != 120*time.Second {
		t.Fatalf("zero max should not cap server delay: delay=%s err=%v", delay, err)
	}

	past := http.Header{}
	past.Set("Retry-After", now.Add(-5*time.Second).UTC().Format(http.TimeFormat))
	delay, err = providerRetryDelay(past, 0, maxDelay, now, "boom")
	if err != nil || delay != 0 {
		t.Fatalf("past date delay=%s err=%v", delay, err)
	}
}

func TestApplyHTTPRetry(t *testing.T) {
	h := http.Header{}
	if !applyHTTPRetry(http.StatusTooManyRequests, h, true, false) ||
		!applyHTTPRetry(http.StatusRequestTimeout, h, true, false) ||
		!applyHTTPRetry(http.StatusConflict, h, true, false) ||
		!applyHTTPRetry(http.StatusServiceUnavailable, h, true, false) {
		t.Fatal("408, 409, 429, and 5xx should retry")
	}
	if applyHTTPRetry(http.StatusBadRequest, h, true, false) {
		t.Fatal("400 without a retryable message should not retry")
	}
	if !applyHTTPRetry(http.StatusBadRequest, h, true, true) {
		t.Fatal("400 with a retryable message should retry")
	}
	h.Set("X-Should-Retry", "false")
	if applyHTTPRetry(http.StatusTooManyRequests, h, true, true) {
		t.Fatal("x-should-retry false should win")
	}
	h.Set("X-Should-Retry", "true")
	if !applyHTTPRetry(http.StatusBadRequest, h, true, false) {
		t.Fatal("x-should-retry true should force a retry")
	}
	if !applyHTTPRetry(http.StatusOK, h, true, true) {
		t.Fatal("2xx should ignore x-should-retry and keep the message classification")
	}
	h.Set("X-Should-Retry", "false")
	if !applyHTTPRetry(http.StatusOK, h, true, true) {
		t.Fatal("2xx should ignore x-should-retry false")
	}
	if applyHTTPRetry(http.StatusInternalServerError, nil, false, false) {
		t.Fatal("a response that was not observed should not retry by status")
	}
}

func TestWrapProviderRetryHonorsRetryAfter(t *testing.T) {
	const ok = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"
	t.Run("seconds", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&calls, 1) == 1 {
				w.Header().Set("Retry-After", "2")
				http.Error(w, "rate limit", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, ok)
		}))
		defer srv.Close()

		fn := WrapProviderRetry((&OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}).StreamFn(), ProviderRetry{
			MaxRetries: 2,
			MaxDelay:   60 * time.Second,
		})
		start := time.Now()
		stream, err := fn(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-4o"})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		elapsed := time.Since(start)
		if final == nil || final.StopReason != StopStop || final.Text() != "ok" {
			t.Fatalf("final=%+v", final)
		}
		if atomic.LoadInt32(&calls) != 2 {
			t.Fatalf("calls=%d", calls)
		}
		if elapsed < 1800*time.Millisecond || elapsed > 4*time.Second {
			t.Fatalf("elapsed=%s", elapsed)
		}
	})

	t.Run("unparseable date", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&calls, 1) == 1 {
				w.Header().Set("Retry-After", "not-a-date")
				http.Error(w, "rate limit", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, ok)
		}))
		defer srv.Close()

		fn := WrapProviderRetry((&OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}).StreamFn(), ProviderRetry{
			MaxRetries: 2,
			MaxDelay:   60 * time.Second,
		})
		start := time.Now()
		stream, err := fn(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-4o"})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		elapsed := time.Since(start)
		if final == nil || final.StopReason != StopStop {
			t.Fatalf("final=%+v", final)
		}
		if atomic.LoadInt32(&calls) != 2 {
			t.Fatalf("calls=%d", calls)
		}
		if elapsed < 300*time.Millisecond || elapsed > 1500*time.Millisecond {
			t.Fatalf("elapsed=%s", elapsed)
		}
	})

	t.Run("exceeds max", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.Header().Set("Retry-After", "120")
			http.Error(w, "rate limit", http.StatusTooManyRequests)
		}))
		defer srv.Close()

		fn := WrapProviderRetry((&OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}).StreamFn(), ProviderRetry{
			MaxRetries: 2,
			MaxDelay:   60 * time.Second,
		})
		start := time.Now()
		stream, err := fn(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-4o"})
		if err != nil {
			t.Fatal(err)
		}
		_, final := stream.Collect()
		elapsed := time.Since(start)
		if final == nil || !strings.Contains(final.ErrorMessage, "Server requested 120s retry delay (max: 60s)") {
			t.Fatalf("final=%+v", final)
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Fatalf("calls=%d", calls)
		}
		if elapsed > time.Second {
			t.Fatalf("waited %s", elapsed)
		}
	})
}
