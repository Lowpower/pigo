package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }

func errorReply(msg string) ai.StreamFn {
	return func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{
			Role:         ai.RoleAssistant,
			StopReason:   ai.StopError,
			ErrorMessage: msg,
		}), nil
	}
}

func scriptedReplies(replies ...ai.StreamFn) ai.StreamFn {
	var n int32
	return func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		i := int(atomic.AddInt32(&n, 1) - 1)
		if i >= len(replies) {
			i = len(replies) - 1
		}
		return replies[i](ctx, req, opts)
	}
}

func retryEngine(t *testing.T, stream ai.StreamFn) *Engine {
	t.Helper()
	e := &Engine{
		Stream:   stream,
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts: Options{Config: config.Config{
			Provider: "anthropic",
			Model:    "claude-sonnet-4",
			Retry: config.RetrySettings{
				Enabled:     boolPtr(true),
				MaxRetries:  intPtr(3),
				BaseDelayMs: intPtr(1),
			},
		}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	return e
}

func TestRPCRetriesTransientErrorAndSucceeds(t *testing.T) {
	var calls int32
	var secondReq ai.Context
	stream := scriptedReplies(
		func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			atomic.AddInt32(&calls, 1)
			return errorReply("overloaded_error")(ctx, req, opts)
		},
		func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			atomic.AddInt32(&calls, 1)
			secondReq = req
			return textReply("recovered")(ctx, req, opts)
		},
	)
	e := retryEngine(t, stream)

	in := bytes.NewBufferString(`{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	rows := decodeRPCRows(t, out.String())
	starts := rpcRowsOfType(rows, "auto_retry_start")
	if len(starts) != 1 {
		t.Fatalf("auto_retry_start = %d in %s", len(starts), out.String())
	}
	if starts[0]["attempt"] != float64(1) || starts[0]["maxAttempts"] != float64(3) {
		t.Fatalf("start payload = %v", starts[0])
	}
	if starts[0]["errorMessage"] != "overloaded_error" {
		t.Fatalf("errorMessage = %v", starts[0]["errorMessage"])
	}
	ends := rpcRowsOfType(rows, "auto_retry_end")
	if len(ends) != 1 || ends[0]["success"] != true {
		t.Fatalf("auto_retry_end = %v in %s", ends, out.String())
	}
	agentEnds := rpcRowsOfType(rows, "agent_end")
	if len(agentEnds) != 2 {
		t.Fatalf("agent_end count = %d", len(agentEnds))
	}
	if agentEnds[0]["willRetry"] != true || agentEnds[1]["willRetry"] != false {
		t.Fatalf("willRetry = %v %v", agentEnds[0]["willRetry"], agentEnds[1]["willRetry"])
	}
	if len(rpcRowsOfType(rows, "agent_settled")) != 1 {
		t.Fatalf("agent_settled count in %s", out.String())
	}
	for _, m := range secondReq.Messages {
		if m.Role == ai.RoleAssistant && m.Assistant != nil && m.Assistant.StopReason == ai.StopError {
			t.Fatalf("retry request still contains error assistant: %+v", m)
		}
	}
}

func TestRPCExhaustsMaxRetries(t *testing.T) {
	var calls int32
	e := retryEngine(t, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		atomic.AddInt32(&calls, 1)
		return errorReply("overloaded_error")(ctx, req, opts)
	})
	e.Opts.Config.Retry.MaxRetries = intPtr(2)

	in := bytes.NewBufferString(`{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	rows := decodeRPCRows(t, out.String())
	starts := rpcRowsOfType(rows, "auto_retry_start")
	if len(starts) != 2 {
		t.Fatalf("auto_retry_start = %d in %s", len(starts), out.String())
	}
	ends := rpcRowsOfType(rows, "auto_retry_end")
	if len(ends) != 1 || ends[0]["success"] != false {
		t.Fatalf("auto_retry_end = %v", ends)
	}
	if ends[0]["attempt"] != float64(2) {
		t.Fatalf("failure attempt = %v", ends[0]["attempt"])
	}
	will := make([]any, 0, 3)
	for _, ev := range rpcRowsOfType(rows, "agent_end") {
		will = append(will, ev["willRetry"])
	}
	if len(will) != 3 || will[0] != true || will[1] != true || will[2] != false {
		t.Fatalf("willRetry sequence = %v", will)
	}
}

func TestRPCDoesNotRetryWhenDisabled(t *testing.T) {
	var calls int32
	e := retryEngine(t, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		atomic.AddInt32(&calls, 1)
		return errorReply("overloaded_error")(ctx, req, opts)
	})

	in := bytes.NewBufferString(`{"type":"set_auto_retry","enabled":false,"id":"r1"}
{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	rows := decodeRPCRows(t, out.String())
	if len(rpcRowsOfType(rows, "auto_retry_start")) != 0 {
		t.Fatalf("unexpected retry in %s", out.String())
	}
	var sawSet bool
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "set_auto_retry" {
			sawSet = true
			if r["success"] != true {
				t.Fatalf("set_auto_retry = %v", r)
			}
		}
	}
	if !sawSet {
		t.Fatalf("missing set_auto_retry response in %s", out.String())
	}
}

func TestRPCDoesNotRetryNonRetryableError(t *testing.T) {
	var calls int32
	e := retryEngine(t, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		atomic.AddInt32(&calls, 1)
		return errorReply("invalid_api_key")(ctx, req, opts)
	})
	in := bytes.NewBufferString(`{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if len(rpcRowsOfType(decodeRPCRows(t, out.String()), "auto_retry_start")) != 0 {
		t.Fatalf("unexpected retry in %s", out.String())
	}
}

func TestRPCAbortRetryCancelsBackoff(t *testing.T) {
	var calls int32
	e := retryEngine(t, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		atomic.AddInt32(&calls, 1)
		return errorReply("overloaded_error")(ctx, req, opts)
	})
	e.Opts.Config.Retry.BaseDelayMs = intPtr(500)

	pr, pw := io.Pipe()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- e.ServeRPC(context.Background(), pr, &out) }()

	enc := json.NewEncoder(pw)
	if err := enc.Encode(map[string]any{"type": "prompt", "message": "hi", "id": "p1"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for auto_retry_start")
		}
		if len(rpcRowsOfType(decodeRPCRows(t, out.String()), "auto_retry_start")) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := enc.Encode(map[string]any{"type": "abort_retry", "id": "a1"}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{"type": "quit"}); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeRPC did not return")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	rows := decodeRPCRows(t, out.String())
	var sawAbort bool
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "abort_retry" {
			sawAbort = true
			if r["success"] != true {
				t.Fatalf("abort_retry = %v", r)
			}
		}
	}
	if !sawAbort {
		t.Fatalf("missing abort_retry response in %s", out.String())
	}
	var cancelled bool
	for _, ev := range rpcRowsOfType(rows, "auto_retry_end") {
		if ev["success"] == false && ev["finalError"] == "Retry cancelled" {
			cancelled = true
		}
	}
	if !cancelled {
		t.Fatalf("missing cancelled auto_retry_end in %s", out.String())
	}
}

func TestRPCRetryPersistsErrorThenSuccess(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	e := retryEngine(t, scriptedReplies(errorReply("overloaded_error"), textReply("recovered")))
	e.Opts.Session = sess

	in := bytes.NewBufferString(`{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	msgs := session.RestoreAIMessages(sess.Entries())
	if len(msgs) < 3 {
		t.Fatalf("session messages = %d (%+v), want user + error + success", len(msgs), msgs)
	}
	var sawError, sawSuccess bool
	for _, m := range msgs {
		if m.Role == ai.RoleAssistant && m.Assistant != nil && m.Assistant.StopReason == ai.StopError {
			sawError = true
		}
		if m.Role == ai.RoleAssistant && m.Content == "recovered" {
			sawSuccess = true
		}
		if m.Assistant != nil && m.Assistant.Text() == "recovered" {
			sawSuccess = true
		}
	}
	if !sawError || !sawSuccess {
		t.Fatalf("session missing error or success: %+v", msgs)
	}
}
