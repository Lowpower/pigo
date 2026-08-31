package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
)

func scriptedAssistant(stop ai.StopReason, errMsg, text string) ai.StreamFn {
	return func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		msg := &ai.AssistantMessage{
			Role:         ai.RoleAssistant,
			StopReason:   stop,
			ErrorMessage: errMsg,
		}
		if text != "" {
			msg.Content = []*ai.Content{{Type: ai.KindText, Text: text}}
		}
		return ai.EmitMessage(ctx, msg), nil
	}
}

func compactableMsgs() []ai.Message {
	return []ai.Message{
		{Role: ai.RoleUser, Content: "old-turn-one"},
		{Role: ai.RoleAssistant, Content: "old-turn-two"},
		{Role: ai.RoleUser, Content: "keep"},
	}
}

func retryCfg() config.Config {
	enabled := true
	maxRetries := 3
	delay := 1
	keep := 1
	return config.Config{
		Model:            "test",
		KeepRecentTokens: keep,
		Retry: config.RetrySettings{
			Enabled:     &enabled,
			MaxRetries:  &maxRetries,
			BaseDelayMs: &delay,
		},
	}
}

func eventTypes(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		typ, _ := ev["type"].(string)
		out = append(out, typ)
	}
	return out
}

func TestCompactNowRetriesTransientSummarizationError(t *testing.T) {
	var calls int32
	e := &Engine{
		Opts: Options{Config: retryCfg()},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return scriptedAssistant(ai.StopError, "terminated", "")(ctx, req, opts)
			}
			return scriptedAssistant(ai.StopStop, "", "## Goal\nRecovered.")(ctx, req, opts)
		},
	}
	var events []map[string]any
	e.onSessionEvent = func(v any) {
		row, _ := v.(map[string]any)
		events = append(events, row)
	}
	out, summary, err := e.CompactNow(context.Background(), compactableMsgs(), "")
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("calls=%d", calls)
	}
	if summary == "" || len(out) == 0 {
		t.Fatalf("summary=%q out=%d", summary, len(out))
	}
	types := eventTypes(events)
	if !containsAll(types, "compaction_start", "summarization_retry_scheduled", "summarization_retry_attempt_start", "summarization_retry_finished", "compaction_end") {
		t.Fatalf("events=%v", types)
	}
	var scheduled, start map[string]any
	for _, ev := range events {
		switch ev["type"] {
		case "summarization_retry_scheduled":
			scheduled = ev
		case "summarization_retry_attempt_start":
			start = ev
		}
	}
	if scheduled["attempt"] != 1 || scheduled["maxAttempts"] != 3 || scheduled["delayMs"] != 1 {
		t.Fatalf("scheduled=%v", scheduled)
	}
	if start["source"] != "compaction" || start["reason"] != "manual" {
		t.Fatalf("start=%v", start)
	}
}

func TestCompactNowDoesNotRetryAbort(t *testing.T) {
	var calls int32
	e := &Engine{
		Opts: Options{Config: retryCfg()},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			atomic.AddInt32(&calls, 1)
			return scriptedAssistant(ai.StopAborted, "terminated", "")(ctx, req, opts)
		},
	}
	var events []map[string]any
	e.onSessionEvent = func(v any) {
		row, _ := v.(map[string]any)
		events = append(events, row)
	}
	_, _, err := e.CompactNow(context.Background(), compactableMsgs(), "")
	if err == nil {
		t.Fatal("expected abort error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls=%d", calls)
	}
	for _, typ := range eventTypes(events) {
		if typ == "summarization_retry_scheduled" {
			t.Fatalf("abort should not retry: %v", eventTypes(events))
		}
	}
}

func TestCompactNowDoesNotRetryNonRetryableError(t *testing.T) {
	var calls int32
	e := &Engine{
		Opts: Options{Config: retryCfg()},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			atomic.AddInt32(&calls, 1)
			return scriptedAssistant(ai.StopError, "insufficient_quota", "")(ctx, req, opts)
		},
	}
	var events []map[string]any
	e.onSessionEvent = func(v any) {
		row, _ := v.(map[string]any)
		events = append(events, row)
	}
	_, _, err := e.CompactNow(context.Background(), compactableMsgs(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls=%d", calls)
	}
	for _, typ := range eventTypes(events) {
		if typ == "summarization_retry_scheduled" {
			t.Fatalf("quota should not retry: %v", eventTypes(events))
		}
	}
}

func TestNavigateTreeRetriesBranchSummary(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "a"})
	a, _ := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "b"})
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "c"})
	_, _ = sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "d"})
	var calls int32
	e := &Engine{
		Opts: Options{Session: sess, Config: retryCfg()},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return scriptedAssistant(ai.StopError, "overloaded", "")(ctx, req, opts)
			}
			return scriptedAssistant(ai.StopStop, "", "## Goal\nBranch.")(ctx, req, opts)
		},
	}
	var events []map[string]any
	e.onSessionEvent = func(v any) {
		row, _ := v.(map[string]any)
		events = append(events, row)
	}
	if _, err := e.NavigateTree(context.Background(), a.ID, session.NavigateOpts{Summarize: true}); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("calls=%d", calls)
	}
	var start map[string]any
	for _, ev := range events {
		if ev["type"] == "summarization_retry_attempt_start" {
			start = ev
		}
	}
	if start["source"] != "branchSummary" {
		t.Fatalf("start=%v events=%v", start, eventTypes(events))
	}
}

func containsAll(have []string, want ...string) bool {
	seen := map[string]bool{}
	for _, h := range have {
		seen[h] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}
