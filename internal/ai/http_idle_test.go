package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientIdleAllowsContinuousStream(t *testing.T) {
	SetHTTPIdleTimeout(400 * time.Millisecond)
	t.Cleanup(func() { SetHTTPIdleTimeout(5 * time.Minute) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 8; i++ {
			if _, err := io.WriteString(w, "x"); err != nil {
				return
			}
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)

	client := httpClient(ClientConfig{})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "xxxxxxxx" {
		t.Fatalf("body = %q", body)
	}
}

func TestHTTPClientIdleTimesOutOnSilence(t *testing.T) {
	SetHTTPIdleTimeout(200 * time.Millisecond)
	t.Cleanup(func() { SetHTTPIdleTimeout(5 * time.Minute) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		flusher.Flush()
		sleepUnlessCanceled(r, 2*time.Second)
		_, _ = io.WriteString(w, "late")
	}))
	t.Cleanup(srv.Close)

	client := httpClient(ClientConfig{})
	start := time.Now()
	resp, err := client.Get(srv.URL)
	if err != nil {
		if !idleTimeoutMessage(err) {
			t.Fatalf("header wait: %v", err)
		}
		if time.Since(start) > time.Second {
			t.Fatalf("header timeout took %s", time.Since(start))
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.ReadAll(resp.Body)
	if err == nil || !idleTimeoutMessage(err) {
		t.Fatalf("body wait: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("body timeout took %s", time.Since(start))
	}
}

func TestHTTPClientIdleTimesOutWaitingForHeaders(t *testing.T) {
	SetHTTPIdleTimeout(200 * time.Millisecond)
	t.Cleanup(func() { SetHTTPIdleTimeout(5 * time.Minute) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sleepUnlessCanceled(r, 2*time.Second)
		_, _ = io.WriteString(w, "late")
	}))
	t.Cleanup(srv.Close)

	client := httpClient(ClientConfig{})
	start := time.Now()
	resp, err := client.Get(srv.URL)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !idleTimeoutMessage(err) {
		t.Fatalf("header wait: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("header timeout took %s", time.Since(start))
	}
}

func TestHTTPClientIdleZeroDisablesTimeout(t *testing.T) {
	SetHTTPIdleTimeout(0)
	t.Cleanup(func() { SetHTTPIdleTimeout(5 * time.Minute) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		flusher.Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)

	client := httpClient(ClientConfig{})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestHTTPClientIdleContextCancel(t *testing.T) {
	SetHTTPIdleTimeout(5 * time.Second)
	t.Cleanup(func() { SetHTTPIdleTimeout(5 * time.Minute) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		flusher.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := httpClient(ClientConfig{})
	start := time.Now()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	resp, err := client.Do(req)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("do: %v", err)
		}
		if time.Since(start) > time.Second {
			t.Fatalf("cancel took %s", time.Since(start))
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read: %v", err)
	}
	if idleTimeoutMessage(err) {
		t.Fatalf("cancel reported as idle timeout: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("cancel took %s", time.Since(start))
	}
}

func TestOpenAINilClientUsesIdleTimeout(t *testing.T) {
	SetHTTPIdleTimeout(200 * time.Millisecond)
	t.Cleanup(func() { SetHTTPIdleTimeout(5 * time.Minute) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		sleepUnlessCanceled(r, 2*time.Second)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
	}))
	t.Cleanup(srv.Close)

	client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k"}
	stream, err := client.StreamFn()(context.Background(), Context{}, Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || !idleTimeoutMessage(errors.New(final.ErrorMessage)) {
		msg := ""
		if final != nil {
			msg = final.ErrorMessage
		}
		t.Fatalf("message = %q", msg)
	}
}

func sleepUnlessCanceled(r *http.Request, d time.Duration) {
	select {
	case <-time.After(d):
	case <-r.Context().Done():
	}
}

func idleTimeoutMessage(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "httpIdleTimeoutMs") && strings.Contains(strings.ToLower(msg), "idle")
}
