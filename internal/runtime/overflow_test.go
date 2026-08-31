package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestPrintJSONOverflowCompactsAndRetriesOnce(t *testing.T) {
	var calls int32
	e := &Engine{
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts: Options{Config: config.Config{
			Provider:         "anthropic",
			Model:            "claude-sonnet-4",
			KeepRecentTokens: 1,
			Retry: config.RetrySettings{
				Enabled:     boolPtr(true),
				MaxRetries:  intPtr(3),
				BaseDelayMs: intPtr(1),
			},
		}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			n := atomic.AddInt32(&calls, 1)
			switch n {
			case 1:
				return errorReply("prompt is too long: 300000 tokens > 200000 maximum")(ctx, req, opts)
			case 2:
				return textReply("## Goal\nCompacted.")(ctx, req, opts)
			default:
				return textReply("recovered")(ctx, req, opts)
			}
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	var out bytes.Buffer
	hist := compactableMsgs()
	if err := e.PrintJSON(context.Background(), &out, hist, "next", nil); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Fatalf("calls=%d want at least overflow+summary+retry", calls)
	}
	s := out.String()
	if !strings.Contains(s, `"reason":"overflow"`) {
		t.Fatalf("missing overflow compaction:\n%s", s)
	}
	if strings.Contains(s, `"type":"auto_retry_start"`) {
		t.Fatalf("overflow must not use auto_retry:\n%s", s)
	}
	if !strings.Contains(s, "recovered") {
		t.Fatalf("missing recovered text:\n%s", s)
	}
	var sawRetryTrue, sawRetryFalse bool
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	for dec.More() {
		var ev map[string]any
		if dec.Decode(&ev) != nil {
			break
		}
		if ev["type"] == "agent_end" {
			if ev["willRetry"] == true {
				sawRetryTrue = true
			}
			if ev["willRetry"] == false {
				sawRetryFalse = true
			}
		}
	}
	if !sawRetryTrue || !sawRetryFalse {
		t.Fatalf("willRetry true then false missing:\n%s", s)
	}
}

func TestPrintJSONRecoverableLengthCompactsAndRetriesOnce(t *testing.T) {
	var calls int32
	e := &Engine{
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts: Options{Config: config.Config{
			Provider:         "anthropic",
			Model:            "claude-sonnet-4",
			KeepRecentTokens: 1,
		}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			n := atomic.AddInt32(&calls, 1)
			switch n {
			case 1:
				return ai.EmitMessage(ctx, &ai.AssistantMessage{
					Role:       ai.RoleAssistant,
					StopReason: ai.StopLength,
					Usage:      ai.Usage{Output: 10},
					Content:    []*ai.Content{{Type: ai.KindText, Text: "truncated"}},
				}), nil
			case 2:
				return textReply("## Goal\nCompacted.")(ctx, req, opts)
			default:
				return textReply("recovered")(ctx, req, opts)
			}
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	var out bytes.Buffer
	if err := e.PrintJSON(context.Background(), &out, compactableMsgs(), "next", nil); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Fatalf("calls=%d want overflow+summary+retry", calls)
	}
	s := out.String()
	if !strings.Contains(s, `"reason":"overflow"`) {
		t.Fatalf("missing overflow compaction:\n%s", s)
	}
	if strings.Contains(s, `"type":"auto_retry_start"`) {
		t.Fatalf("length recovery must not use auto_retry:\n%s", s)
	}
	if !strings.Contains(s, "recovered") {
		t.Fatalf("missing recovered text:\n%s", s)
	}
}

func TestPrintJSONOverflowDoesNotLoop(t *testing.T) {
	var calls int32
	e := &Engine{
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts: Options{Config: config.Config{
			Model:            "x",
			KeepRecentTokens: 1,
		}},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			n := atomic.AddInt32(&calls, 1)
			if n%2 == 1 {
				return errorReply("prompt is too long")(ctx, req, opts)
			}
			return textReply("## Goal\nCompacted.")(ctx, req, opts)
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	var out bytes.Buffer
	err := e.PrintJSON(context.Background(), &out, compactableMsgs(), "next", nil)
	if err == nil {
		t.Fatal("expected overflow to remain an error after one retry")
	}
	if atomic.LoadInt32(&calls) > 6 {
		t.Fatalf("looped too many times: calls=%d", calls)
	}
	if !strings.Contains(out.String(), "recovery failed") && !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
}
