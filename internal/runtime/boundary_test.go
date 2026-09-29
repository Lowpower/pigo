package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
)

func TestTurnEndContextEditContinuesOnce(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "once")
	h := spawnRuntimeExt(t, "boundary", nil, "PIGO_BOUNDARY=turn", "PIGO_BOUNDARY_MARK="+mark)
	sess := session.New(t.TempDir(), t.TempDir())
	var calls int32
	var second []ai.Message
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts: Options{
			Session: sess,
			Config:  config.Config{Model: "x", Provider: "mock"},
		},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return textReply("one")(ctx, req, opts)
			}
			second = append([]ai.Message(nil), req.Messages...)
			return textReply("two")(ctx, req, opts)
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	events := e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if calls != 2 {
		t.Fatalf("provider calls = %d", calls)
	}
	for _, m := range second {
		if m.Text() == "one" || m.Content == "one" {
			t.Fatalf("omitted assistant still in the next request: %+v", second)
		}
	}
	var sawEdit, sawRaw bool
	for _, entry := range sess.Entries() {
		if entry.Type == "context_edit" {
			sawEdit = true
		}
		if entry.Type == "message" && strings.Contains(string(entry.Message), "one") {
			sawRaw = true
		}
	}
	if !sawEdit || !sawRaw {
		t.Fatalf("edit=%v raw=%v entries=%d", sawEdit, sawRaw, len(sess.Entries()))
	}
	var last []agent.Msg
	for _, ev := range events {
		if ev.Type == agent.EventAgentEnd {
			last = ev.Messages
		}
	}
	before := len(sess.Entries())
	e.PersistTranscript(last)
	if len(sess.Entries()) != before {
		t.Fatalf("persisted again: %d -> %d", before, len(sess.Entries()))
	}
}

func TestTurnEndInvalidEntryDoesNotContinue(t *testing.T) {
	h := spawnRuntimeExt(t, "boundary", nil, "PIGO_BOUNDARY=bad")
	var calls int32
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts: Options{
			Session: session.New(t.TempDir(), t.TempDir()),
			Config:  config.Config{Model: "x", Provider: "mock"},
		},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			atomic.AddInt32(&calls, 1)
			return textReply("one")(ctx, req, opts)
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if calls != 1 {
		t.Fatalf("provider calls = %d", calls)
	}
}

func TestTurnEndErrorIgnoresContinue(t *testing.T) {
	h := spawnRuntimeExt(t, "boundary", nil, "PIGO_BOUNDARY=error")
	var calls int32
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts: Options{
			Session: session.New(t.TempDir(), t.TempDir()),
			Config:  config.Config{Model: "x", Provider: "mock"},
		},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			atomic.AddInt32(&calls, 1)
			return errorReply("boom")(ctx, req, opts)
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if calls != 1 {
		t.Fatalf("provider calls = %d", calls)
	}
}

func TestBeforeSettleAppendsAndContinuesOnce(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "once")
	h := spawnRuntimeExt(t, "boundary", nil, "PIGO_BOUNDARY=settle", "PIGO_BOUNDARY_MARK="+mark)
	sess := session.New(t.TempDir(), t.TempDir())
	var calls int32
	var second []ai.Message
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts: Options{
			Session: sess,
			Config:  config.Config{Model: "x", Provider: "mock"},
		},
		Stream: func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
			n := atomic.AddInt32(&calls, 1)
			if n > 1 {
				second = append([]ai.Message(nil), req.Messages...)
			}
			return textReply("done")(ctx, req, opts)
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if calls != 1 {
		t.Fatalf("calls before settle = %d", calls)
	}
	if !e.BeforeSettle(context.Background()) {
		t.Fatal("expected continue")
	}
	_ = e.Continue(context.Background(), e.History()).Collect()
	if calls != 2 {
		t.Fatalf("calls after continue = %d", calls)
	}
	if e.BeforeSettle(context.Background()) {
		t.Fatal("second settle continued")
	}
	found := false
	for _, m := range second {
		if m.Content == "review please" {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom message missing from next request: %+v", second)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Fatal(err)
	}
}

func TestBoundaryDraftsAppendAllowedEntries(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	user, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Opts: Options{Session: sess}}
	raw := []any{
		map[string]any{"type": "custom", "customType": "note", "data": map[string]any{"n": 1}},
		map[string]any{"type": "custom_message", "customType": "review", "content": "look", "display": false},
		map[string]any{"type": "context_edit", "targetId": user.ID, "replacement": nil},
		map[string]any{"type": "compaction", "summary": "sum", "firstKeptEntryId": nil, "tokensBefore": 4},
	}
	drafts, err := parseBoundaryDrafts(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.validateDrafts(drafts); err != nil {
		t.Fatal(err)
	}
	if err := e.appendDrafts(drafts); err != nil {
		t.Fatal(err)
	}
	var sawCustom, sawMsg, sawEdit, sawCompaction bool
	for _, entry := range sess.Entries() {
		switch entry.Type {
		case "custom":
			sawCustom = entry.CustomType == "note"
		case "custom_message":
			sawMsg = true
		case "context_edit":
			sawEdit = entry.TargetID == user.ID && entry.Replacement != nil && string(entry.Replacement.Raw) == "null"
		case "compaction":
			sawCompaction = entry.FirstKeptEntryID == entry.ID
		}
	}
	if !sawCustom || !sawMsg || !sawEdit || !sawCompaction {
		t.Fatalf("custom=%v msg=%v edit=%v compaction=%v", sawCustom, sawMsg, sawEdit, sawCompaction)
	}
	stored, _ := sess.EntryByID(user.ID)
	if !strings.Contains(string(stored.Message), "secret") {
		t.Fatalf("target rewritten: %s", stored.Message)
	}
	for _, msg := range session.ModelMessages(session.ContextEntries(sess)) {
		if msg.Content == "secret" || msg.Text() == "secret" {
			t.Fatalf("omitted target still in model context: %+v", msg)
		}
	}
}
