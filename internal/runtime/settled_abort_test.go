package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/tools"
)

func settledRows(t *testing.T, raw string) []map[string]any {
	t.Helper()
	rows := rpcRowsOfType(decodeRPCRows(t, raw), "agent_settled")
	if len(rows) != 1 {
		t.Fatalf("agent_settled count = %d in %s", len(rows), raw)
	}
	return rows
}

func readSettledLog(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			break
		}
		out = append(out, row)
	}
	return out
}

func wantSettledPayload(t *testing.T, path string, aborted bool) {
	t.Helper()
	rows := readSettledLog(t, path)
	if len(rows) != 1 {
		t.Fatalf("extension agent_settled count = %d (%s)", len(rows), path)
	}
	if len(rows[0]) != 1 || rows[0]["aborted"] != aborted {
		t.Fatalf("payload = %#v, want aborted=%v only", rows[0], aborted)
	}
}

func TestPrintJSONSettledAbortedFalse(t *testing.T) {
	e := newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}})
	wireQueues(e)
	var out bytes.Buffer
	if err := e.PrintJSON(context.Background(), &out, nil, "hi", nil); err != nil {
		t.Fatal(err)
	}
	raw := out.String()
	settled := settledRows(t, raw)
	if settled[0]["aborted"] != false {
		t.Fatalf("aborted = %#v in %s", settled[0]["aborted"], raw)
	}
	for _, end := range rpcRowsOfType(decodeRPCRows(t, raw), "agent_end") {
		if _, ok := end["aborted"]; ok {
			t.Fatalf("agent_end gained aborted: %#v", end)
		}
	}
}

func TestPrintJSONErrorIsNotAborted(t *testing.T) {
	e := &Engine{
		Stream:   errorReply("invalid_api_key"),
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	wireQueues(e)
	var out bytes.Buffer
	if err := e.PrintJSON(context.Background(), &out, nil, "hi", nil); err == nil {
		t.Fatal("expected error")
	}
	settled := settledRows(t, out.String())
	if settled[0]["aborted"] != false {
		t.Fatalf("aborted = %#v in %s", settled[0]["aborted"], out.String())
	}
}

func TestStopReasonAbortedWithoutCancelIsFalse(t *testing.T) {
	e := &Engine{
		Stream: func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
			return ai.EmitMessage(ctx, &ai.AssistantMessage{
				Role:         ai.RoleAssistant,
				StopReason:   ai.StopAborted,
				ErrorMessage: "stream closed",
			}), nil
		},
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	wireQueues(e)
	var out bytes.Buffer
	_ = e.PrintJSON(context.Background(), &out, nil, "hi", nil)
	settled := settledRows(t, out.String())
	if settled[0]["aborted"] != false {
		t.Fatalf("stopReason aborted without a cancel: %#v", settled[0])
	}
}

func TestPrintJSONCancelledContextSettlesAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int32
	e := retryEngine(t, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		atomic.AddInt32(&calls, 1)
		return errorReply("overloaded_error")(ctx, req, opts)
	})
	var out bytes.Buffer
	_ = e.PrintJSON(ctx, &out, nil, "hi", nil)
	// The agent loop races a cancelled context against the first send, so the
	// provider may or may not be entered. It must not be retried.
	if n := atomic.LoadInt32(&calls); n > 1 {
		t.Fatalf("calls = %d, want at most 1", n)
	}
	raw := out.String()
	rows := decodeRPCRows(t, raw)
	if len(rpcRowsOfType(rows, "auto_retry_start")) != 0 {
		t.Fatalf("cancelled run retried: %s", raw)
	}
	settled := settledRows(t, raw)
	if settled[0]["aborted"] != true {
		t.Fatalf("aborted = %#v in %s", settled[0]["aborted"], raw)
	}
}

func TestRPCAbortSettlesAborted(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	e := &Engine{
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			select {
			case <-release:
			case <-ctx.Done():
				return ai.EmitMessage(ctx, &ai.AssistantMessage{
					Role:         ai.RoleAssistant,
					StopReason:   ai.StopError,
					ErrorMessage: "invalid_api_key",
				}), nil
			}
			return textReply("pong")(ctx, req, opts)
		},
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	wireQueues(e)
	c := startRPC(t, e)
	c.send(map[string]any{"type": "prompt", "message": "hi", "id": "p1"})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider was not called")
	}
	c.send(map[string]any{"type": "abort", "id": "a1"})
	deadline := time.Now().Add(2 * time.Second)
	var settled map[string]any
	for settled == nil && time.Now().Before(deadline) {
		for _, row := range c.rows() {
			if row["type"] == "agent_settled" {
				settled = row
			}
		}
		if settled == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}
	close(release)
	out := c.close()
	if settled == nil || settled["aborted"] != true {
		t.Fatalf("settled = %#v in %s", settled, out)
	}
}

func TestIdleAbortDoesNotMarkNextRun(t *testing.T) {
	e := newPongEngine(Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}})
	wireQueues(e)
	e.HandleHostCall(nil, "abort", nil)
	var out bytes.Buffer
	if err := e.PrintJSON(context.Background(), &out, nil, "hi", nil); err != nil {
		t.Fatal(err)
	}
	settled := settledRows(t, out.String())
	if settled[0]["aborted"] != false {
		t.Fatalf("idle abort stuck: %#v", settled[0])
	}
}

func TestExtensionSettledPayloadOnError(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "settled.log")
	h := spawnRuntimeExt(t, "boundary", nil, "PIGO_BOUNDARY=always", "PIGO_SETTLED_LOG="+logPath)
	e := settledExtEngine(h, errorReply("invalid_api_key"))
	var out bytes.Buffer
	if err := e.PrintText(context.Background(), &out, nil, "hi", nil); err == nil {
		t.Fatal("expected error")
	}
	wantSettledPayload(t, logPath, false)
}

func TestExtensionSettledPayloadOnCancel(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "settled.log")
	h := spawnRuntimeExt(t, "boundary", nil, "PIGO_BOUNDARY=always", "PIGO_SETTLED_LOG="+logPath)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var calls int32
	e := settledExtEngine(h, func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		atomic.AddInt32(&calls, 1)
		close(started)
		<-ctx.Done()
		return ai.EmitMessage(ctx, &ai.AssistantMessage{
			Role:         ai.RoleAssistant,
			StopReason:   ai.StopError,
			ErrorMessage: "invalid_api_key",
		}), nil
	})
	done := make(chan struct{})
	var out bytes.Buffer
	go func() {
		_ = e.PrintJSON(ctx, &out, nil, "hi", nil)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider was not called")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not settle")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	wantSettledPayload(t, logPath, true)
	if settledRows(t, out.String())[0]["aborted"] != true {
		t.Fatalf("json settled = %s", out.String())
	}
}

func TestAbortFlagSuppressesContinue(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "settled.log")
	h := spawnRuntimeExt(t, "boundary", nil, "PIGO_BOUNDARY=always", "PIGO_SETTLED_LOG="+logPath)
	e := settledExtEngine(h, textReply("pong"))
	e.BeginUserRun()
	cancel := e.SetRunAbort(func() {})
	cancel()
	if e.BeforeSettle(context.Background()) {
		t.Fatal("abort should not continue")
	}
	wantSettledPayload(t, logPath, true)
}

func TestAfterAgentEndSkipsRetryWhenAbortRequested(t *testing.T) {
	e := retryEngine(t, errorReply("overloaded_error"))
	e.BeginUserRun()
	cancel := e.SetRunAbort(func() {})
	cancel()
	last := []agent.Msg{{
		Role: agent.RoleAssistant,
		Assistant: &ai.AssistantMessage{
			Role:         ai.RoleAssistant,
			StopReason:   ai.StopError,
			ErrorMessage: "overloaded_error",
		},
	}}
	if _, again := e.AfterAgentEnd(context.Background(), last); again {
		t.Fatal("retried after abort")
	}
}

func settledExtEngine(h *ext.Host, stream ai.StreamFn) *Engine {
	e := &Engine{
		Hosts:    []*ext.Host{h},
		Stream:   stream,
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}},
	}
	wireQueues(e)
	return e
}
