package cachewarm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

func TestDelayKeepsMargin(t *testing.T) {
	d, ok := Delay(300 * time.Second)
	if !ok || d != 270*time.Second {
		t.Fatalf("delay=%s ok=%v", d, ok)
	}
	if _, ok := Delay(10 * time.Second); ok {
		t.Fatal("10s ttl should not warm")
	}
	d, ok = Delay(15 * time.Second)
	if !ok || d != 5*time.Second {
		t.Fatalf("short delay=%s ok=%v", d, ok)
	}
}

func TestReplayableSkipsBudgetThinking(t *testing.T) {
	if !Replayable("anthropic-messages", "off", false) {
		t.Fatal("thinking off")
	}
	if Replayable("anthropic-messages", "high", false) {
		t.Fatal("budget thinking")
	}
	if !Replayable("anthropic-messages", "high", true) {
		t.Fatal("adaptive")
	}
	if !Replayable("openai-responses", "high", false) {
		t.Fatal("other api")
	}
}

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	at := c.now.Add(d)
	if !at.After(c.now) {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, waiter{at: at, ch: ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	var due []chan time.Time
	var rest []waiter
	for _, w := range c.waiters {
		if !w.at.After(now) {
			due = append(due, w.ch)
		} else {
			rest = append(rest, w)
		}
	}
	c.waiters = rest
	c.mu.Unlock()
	for _, ch := range due {
		ch <- now
	}
}

func sonnet() models.Model {
	return models.Model{
		Provider: "anthropic", ID: "claude-sonnet-4", API: "anthropic-messages",
		Cost:        &models.Cost{Input: 3, Output: 15, CacheRead: 0.30, CacheWrite: 3.75},
		PromptCache: &models.PromptCache{Short: 300, Long: 3600},
	}
}

type harness struct {
	clock     *fakeClock
	w         *Warmer
	refreshes int
	recorded  int
	maxTokens int
	mode      string
	current   bool
	decide    func(Decision) (string, error)
	done      chan struct{}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{clock: newClock(), mode: "streaming", current: true, done: make(chan struct{}, 4)}
	h.w = New(Hooks{
		Now:     h.clock.Now,
		After:   h.clock.After,
		Mode:    func() string { return h.mode },
		Current: func() bool { return h.current },
		Decide: func(d Decision) (string, error) {
			if h.decide != nil {
				return h.decide(d)
			}
			return d.Action, nil
		},
		Refresh: func(ctx context.Context, req Request) (ai.Usage, string, error) {
			if !IsRefresh(ctx) {
				t.Errorf("refresh context not marked")
			}
			if req.Options.MaxTokens != 0 && req.Options.MaxTokens != 1 {
				h.maxTokens = req.Options.MaxTokens
			}
			h.refreshes++
			h.done <- struct{}{}
			return ai.Usage{Output: 1, Cost: ai.UsageCost{Total: 0.01}}, "stop", nil
		},
		Record: func(_ ai.Usage, _, _, _ string) {
			h.recorded++
		},
	})
	return h
}

func (h *harness) start(tokens int) {
	h.w.Start(Request{
		Model: sonnet(), PromptTokens: tokens,
		Options: ai.Options{Thinking: "off", CacheRetention: "short"},
	})
}

func (h *harness) waitRefresh() {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-h.done:
			return
		default:
		}
		st := h.w.Status()
		if st.State == "inactive" && st.Reason != "waiting for first request" && st.Reason != "cache warming disabled" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWarmWhenSavingsClearThreshold(t *testing.T) {
	h := newHarness(t)
	h.start(100_000)
	st := h.w.Status()
	if st.State != "scheduled" {
		t.Fatalf("status=%+v", st)
	}
	h.clock.Advance(270 * time.Second)
	h.waitRefresh()
	if h.refreshes != 1 || h.recorded != 1 {
		t.Fatalf("refreshes=%d recorded=%d", h.refreshes, h.recorded)
	}
}

func TestStopBelowThreshold(t *testing.T) {
	h := newHarness(t)
	h.start(100)
	h.clock.Advance(270 * time.Second)
	h.waitRefresh()
	if h.refreshes != 0 {
		t.Fatalf("refreshes=%d", h.refreshes)
	}
	st := h.w.Status()
	if st.State != "inactive" || st.Reason != "expected savings below threshold" {
		t.Fatalf("status=%+v", st)
	}
}

func TestStreamingStopsWhenSettled(t *testing.T) {
	h := newHarness(t)
	h.start(100_000)
	h.w.Settled()
	st := h.w.Status()
	if st.Reason != "agent run settled" {
		t.Fatalf("status=%+v", st)
	}
	h.clock.Advance(270 * time.Second)
	h.waitRefresh()
	if h.refreshes != 0 {
		t.Fatal("settled run refreshed")
	}
}

func TestIdleUsesLowerProbability(t *testing.T) {
	h := newHarness(t)
	h.mode = "idle"
	h.start(100_000)
	h.w.Settled()
	st := h.w.Status()
	if st.State != "scheduled" || st.Decision == nil || st.Decision.Phase != "idle" {
		t.Fatalf("status=%+v", st)
	}
	if st.Decision.ContinuationProbability != idleProbability {
		t.Fatalf("prob=%v", st.Decision.ContinuationProbability)
	}
}

func TestHourSafetyLimit(t *testing.T) {
	h := newHarness(t)
	h.w.Start(Request{
		Model: models.Model{
			Provider: "anthropic", ID: "claude-sonnet-4", API: "anthropic-messages",
			Cost:        sonnet().Cost,
			PromptCache: &models.PromptCache{Short: 50 * 60},
		},
		PromptTokens: 100_000,
		Options:      ai.Options{Thinking: "off"},
	})
	if h.w.Status().State != "scheduled" {
		t.Fatalf("first status=%+v", h.w.Status())
	}
	// 90% of 50 minutes is 45 minutes, still inside the hour. The next slot is not.
	h.clock.Advance(45 * time.Minute)
	h.waitRefresh()
	st := h.w.Status()
	if h.refreshes != 1 {
		t.Fatalf("refreshes=%d status=%+v", h.refreshes, st)
	}
	if st.State != "inactive" || st.Reason != "one-hour safety limit reached" {
		t.Fatalf("status=%+v", st)
	}
}

func TestIdleSafetyLimit(t *testing.T) {
	h := newHarness(t)
	h.mode = "idle"
	h.w.Start(Request{
		Model: models.Model{
			Provider: "anthropic", ID: "claude-sonnet-4", API: "anthropic-messages",
			Cost:        sonnet().Cost,
			PromptCache: &models.PromptCache{Short: 40 * 60},
		},
		PromptTokens: 100_000,
		Options:      ai.Options{Thinking: "off"},
	})
	h.w.Settled()
	st := h.w.Status()
	if st.State != "inactive" || st.Reason != "30-minute idle safety limit reached" {
		t.Fatalf("status=%+v", st)
	}
}

func TestExtensionCanStop(t *testing.T) {
	h := newHarness(t)
	h.decide = func(Decision) (string, error) { return "stop", nil }
	h.start(100_000)
	h.clock.Advance(270 * time.Second)
	h.waitRefresh()
	if h.refreshes != 0 {
		t.Fatal("extension stop still refreshed")
	}
	st := h.w.Status()
	if st.Reason != "stopped by extension" || !st.ExtensionOverride {
		t.Fatalf("status=%+v", st)
	}
}

func TestExtensionErrorKeepsDecision(t *testing.T) {
	h := newHarness(t)
	h.decide = func(_ Decision) (string, error) { return "stop", errors.New("boom") }
	h.start(100_000)
	h.clock.Advance(270 * time.Second)
	h.waitRefresh()
	if h.refreshes != 1 {
		t.Fatalf("refreshes=%d", h.refreshes)
	}
}

func TestLateTimerStops(t *testing.T) {
	h := newHarness(t)
	h.start(100_000)
	// TTL 300s, delay 270s, half of the 30s margin is 15s. Jump past that.
	h.clock.Advance(270*time.Second + 16*time.Second)
	h.waitRefresh()
	st := h.w.Status()
	if h.refreshes != 0 || st.Reason != "cache refresh deadline missed" {
		t.Fatalf("refreshes=%d status=%+v", h.refreshes, st)
	}
}

func TestContextChangeSkipsRefresh(t *testing.T) {
	h := newHarness(t)
	h.start(100_000)
	h.current = false
	h.clock.Advance(270 * time.Second)
	h.waitRefresh()
	if h.refreshes != 0 {
		t.Fatal("changed context refreshed")
	}
	if h.w.Status().Reason != "conversation context changed" {
		t.Fatalf("status=%+v", h.w.Status())
	}
}
