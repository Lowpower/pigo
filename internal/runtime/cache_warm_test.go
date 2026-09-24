package runtime

import (
	"context"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
)

func TestCacheWarmUsageStaysOutOfContext(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		Provider: "anthropic",
		Opts: Options{
			Session: sess,
			Config:  config.Config{Provider: "anthropic", Model: "claude-sonnet-4", CacheWarming: "off"},
		},
	}
	e.warmer = e.newCacheWarmer()
	if got := e.CacheWarmingStatus(); got != "Inactive (cache warming disabled)" {
		t.Fatalf("status=%s", got)
	}
	e.recordCacheWarm(ai.Usage{Input: 5, Output: 1, Cost: ai.UsageCost{Total: 0.2}}, "anthropic", "claude-sonnet-4", "")
	stats := session.CollectStats(sess, nil, 0)
	if stats.Cost != 0.2 || stats.TotalMessages != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	msgs := session.RestoreAIMessages(session.ContextEntries(sess))
	if len(msgs) != 1 {
		t.Fatalf("context messages=%d", len(msgs))
	}
}

func TestObserveDoesNotWarmWhenDisabled(t *testing.T) {
	var calls int
	inner := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		calls++
		if opts.MaxTokens == 1 {
			t.Fatal("refresh ran while warming is off")
		}
		return textReply("ok")(ctx, req, opts)
	}
	e := &Engine{
		Provider: "openai",
		Opts:     Options{Config: config.Config{Provider: "openai", Model: "gpt-4o", CacheWarming: "off"}},
	}
	e.warmer = e.newCacheWarmer()
	e.Stream = e.gatedStream(inner)
	stream, err := e.Stream(context.Background(), ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	}, ai.Options{Provider: "openai", Model: "gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	_, msg := stream.Collect()
	if msg == nil || msg.Text() != "ok" || calls != 1 {
		t.Fatalf("msg=%v calls=%d", msg, calls)
	}
}
