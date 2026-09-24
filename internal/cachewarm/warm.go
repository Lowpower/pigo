// Package cachewarm keeps one prompt-cache entry alive by replaying its request
// with a one-token output cap before the entry expires.
package cachewarm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

const (
	minExpectedSavings = 0.05
	streamingLimit     = 60 * time.Minute
	idleLimit          = 30 * time.Minute
	idleProbability    = 0.15
)

type refreshCtxKey struct{}

// WithRefresh marks a provider call as a cache refresh so it is not treated as a new run.
func WithRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, refreshCtxKey{}, true)
}

// IsRefresh reports whether ctx was marked by WithRefresh.
func IsRefresh(ctx context.Context) bool {
	v, _ := ctx.Value(refreshCtxKey{}).(bool)
	return v
}

// Delay returns when to refresh a cache entry of the given lifetime.
// Entries of 10 seconds or less are not warmed.
func Delay(ttl time.Duration) (time.Duration, bool) {
	if ttl <= 10*time.Second {
		return 0, false
	}
	delay := time.Duration(float64(ttl) * 0.9)
	margin := ttl - 10*time.Second
	if delay > margin {
		delay = margin
	}
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	return delay, true
}

// TTL is the lifetime for the retention tier the request used.
// An empty retention is the short tier. "none" and a missing tier have no lifetime.
func TTL(cache *models.PromptCache, retention string) (time.Duration, bool) {
	if cache == nil {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(retention)) {
	case "none":
		return 0, false
	case "long":
		if cache.Long <= 0 {
			return 0, false
		}
		return time.Duration(cache.Long) * time.Second, true
	default:
		if cache.Short <= 0 {
			return 0, false
		}
		return time.Duration(cache.Short) * time.Second, true
	}
}

// Replayable reports whether a one-token replay leaves the cache key unchanged.
// Anthropic budget thinking derives its budget from the output cap, so those
// requests are not replayed.
func Replayable(api, thinking string, adaptive bool) bool {
	if api != "anthropic-messages" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(thinking)) {
	case "", "off":
		return true
	default:
		return adaptive
	}
}

// Decision is one warm-or-stop outcome.
type Decision struct {
	Phase                   string
	WarmCost                float64
	MissCost                float64
	ContinuationProbability float64
	ExpectedSavings         float64
	EconomicsAvailable      bool
	Action                  string
}

// Status is the scheduler state shown by /session.
type Status struct {
	State             string
	Reason            string
	NextWarmAt        time.Time
	Decision          *Decision
	ExtensionOverride bool
}

// Request is the provider call whose cache entry should stay warm.
type Request struct {
	Model        models.Model
	Context      ai.Context
	Options      ai.Options
	PromptTokens int
	Adaptive     bool
}

// Hooks are the runtime seams the scheduler calls.
type Hooks struct {
	Now          func() time.Time
	After        func(time.Duration) <-chan time.Time
	Mode         func() string
	Current      func() bool
	Decide       func(Decision) (string, error)
	Refresh      func(ctx context.Context, req Request) (ai.Usage, string, error)
	Record       func(usage ai.Usage, provider, model, note string)
	PromptTokens func() int
}

// Warmer schedules at most one refresh chain.
type Warmer struct {
	hooks    Hooks
	mu       sync.Mutex
	run      *active
	inactive Status
}

type active struct {
	req        Request
	ttl        time.Duration
	delay      time.Duration
	started    time.Time
	phase      string
	next       time.Time
	deadline   time.Time
	override   bool
	refreshing bool
	ctx        context.Context
	cancel     context.CancelFunc
	gen        int
}

// New returns a scheduler. Nil clock functions use the real clock.
func New(h Hooks) *Warmer {
	if h.Now == nil {
		h.Now = time.Now
	}
	if h.After == nil {
		h.After = time.After
	}
	if h.Mode == nil {
		h.Mode = func() string { return "streaming" }
	}
	if h.Current == nil {
		h.Current = func() bool { return true }
	}
	if h.Decide == nil {
		h.Decide = func(d Decision) (string, error) { return d.Action, nil }
	}
	w := &Warmer{hooks: h, inactive: Status{State: "inactive", Reason: "waiting for first request"}}
	return w
}

// Start replaces any previous run with request.
func (w *Warmer) Start(req Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.clearLocked()
	mode := w.hooks.Mode()
	if mode == "off" {
		w.inactive = Status{State: "inactive", Reason: "cache warming disabled"}
		return
	}
	if !Replayable(req.Model.API, req.Options.Thinking, req.Adaptive) {
		w.inactive = Status{State: "inactive", Reason: "request cannot be replayed safely"}
		return
	}
	ttl, ok := TTL(req.Model.PromptCache, req.Options.CacheRetention)
	if !ok {
		reason := "cache lifetime unavailable"
		if strings.EqualFold(req.Options.CacheRetention, "none") {
			reason = "request disabled prompt caching"
		}
		w.inactive = Status{State: "inactive", Reason: reason}
		return
	}
	delay, ok := Delay(ttl)
	if !ok {
		w.inactive = Status{State: "inactive", Reason: "cache lifetime unavailable"}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &active{
		req: req, ttl: ttl, delay: delay, started: w.hooks.Now(),
		phase: "streaming", ctx: ctx, cancel: cancel,
	}
	w.run = run
	w.scheduleLocked(run)
}

// Settled ends streaming runs and switches idle runs to the shorter window.
func (w *Warmer) Settled() {
	w.mu.Lock()
	defer w.mu.Unlock()
	run := w.run
	if run == nil {
		return
	}
	if w.hooks.Mode() == "streaming" {
		w.stopLocked("agent run settled", nil, false)
		return
	}
	run.phase = "idle"
	deadline := run.started.Add(idleLimit)
	if run.next.After(deadline) || !w.hooks.Now().Before(deadline) {
		w.stopLocked("30-minute idle safety limit reached", nil, false)
	}
}

// ModeChanged stops a run the new mode no longer allows.
func (w *Warmer) ModeChanged() {
	w.mu.Lock()
	defer w.mu.Unlock()
	run := w.run
	if run == nil {
		return
	}
	if reason := w.modeStop(run); reason != "" {
		w.stopLocked(reason, nil, false)
	}
}

// Cancel stops warming.
func (w *Warmer) Cancel() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopLocked("inactive", nil, false)
}

// Status is the current scheduler state.
func (w *Warmer) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hooks.Mode() == "off" {
		return Status{State: "inactive", Reason: "cache warming disabled"}
	}
	run := w.run
	if run == nil {
		return w.inactive
	}
	if !w.hooks.Current() {
		return Status{State: "inactive", Reason: "conversation context changed"}
	}
	decision := w.evaluate(run)
	if !decision.EconomicsAvailable && !run.refreshing {
		return Status{State: "inactive", Reason: "cache economics unavailable"}
	}
	state := "scheduled"
	if run.refreshing {
		state = "refreshing"
	}
	return Status{
		State: state, NextWarmAt: run.next, Decision: &decision, ExtensionOverride: run.override,
	}
}

func (w *Warmer) scheduleLocked(run *active) {
	run.override = false
	run.refreshing = false
	run.gen++
	gen := run.gen
	run.next = w.hooks.Now().Add(run.delay)
	run.deadline = run.next.Add((run.ttl - run.delay) / 2)
	limit := streamingLimit
	limitReason := "one-hour safety limit reached"
	if run.phase == "idle" {
		limit = idleLimit
		limitReason = "30-minute idle safety limit reached"
	}
	if run.next.After(run.started.Add(limit)) || !w.hooks.Now().Before(run.started.Add(limit)) {
		w.stopLocked(limitReason, nil, false)
		return
	}
	wait := run.next.Sub(w.hooks.Now())
	if wait < 0 {
		wait = 0
	}
	ch := w.hooks.After(wait)
	ctx := run.ctx
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-ch:
		}
		w.refresh(run, gen)
	}()
}

func (w *Warmer) refresh(run *active, gen int) {
	w.mu.Lock()
	if w.run != run || run.gen != gen {
		w.mu.Unlock()
		return
	}
	if w.hooks.Now().After(run.deadline) {
		w.stopLocked("cache refresh deadline missed", nil, false)
		w.mu.Unlock()
		return
	}
	if reason := w.modeStop(run); reason != "" || !w.hooks.Current() {
		if reason == "" {
			reason = "conversation context changed"
		}
		w.stopLocked(reason, nil, false)
		w.mu.Unlock()
		return
	}
	decision := w.evaluate(run)
	action := decision.Action
	w.mu.Unlock()

	if next, err := w.hooks.Decide(decision); err == nil && (next == "warm" || next == "stop") {
		action = next
	}

	w.mu.Lock()
	if w.run != run || run.gen != gen {
		w.mu.Unlock()
		return
	}
	if w.hooks.Now().After(run.deadline) {
		w.stopLocked("cache refresh deadline missed", nil, false)
		w.mu.Unlock()
		return
	}
	override := action != decision.Action
	if action == "stop" {
		reason := "expected savings below threshold"
		if !decision.EconomicsAvailable {
			reason = "cache economics unavailable"
		}
		if override {
			reason = "stopped by extension"
		}
		w.stopLocked(reason, &decision, override)
		w.mu.Unlock()
		return
	}
	run.override = override
	run.refreshing = true
	req := run.req
	req.Options.MaxTokens = 1
	ctx := run.ctx
	w.mu.Unlock()

	usage, stop, err := w.hooks.Refresh(WithRefresh(ctx), req)

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.run != run || run.gen != gen {
		return
	}
	run.refreshing = false
	if err == nil && stop != string(ai.StopError) && stop != string(ai.StopAborted) && w.hooks.Record != nil {
		note := ""
		if override {
			note = "extension override"
		}
		w.hooks.Record(usage, req.Model.Provider, req.Model.ID, note)
	}
	if w.run == run {
		w.scheduleLocked(run)
	}
}

func (w *Warmer) evaluate(run *active) Decision {
	tokens := run.req.PromptTokens
	if w.hooks.PromptTokens != nil {
		if n := w.hooks.PromptTokens(); n > 0 {
			tokens = n
		}
	}
	cost := run.req.Model.Cost
	var hit, miss, warm float64
	if cost != nil {
		hit = price(tokens, cost.CacheRead)
		if cost.CacheWrite > 0 {
			miss = price(tokens, cost.CacheWrite)
		} else {
			miss = price(tokens, cost.Input)
		}
		warm = price(tokens, cost.CacheRead) + price(1, cost.Output)
	}
	missCost := miss - hit
	if missCost < 0 {
		missCost = 0
	}
	prob := 1.0
	if run.phase == "idle" {
		prob = idleProbability
	}
	available := tokens > 0 && (hit > 0 || miss > 0)
	expected := prob*missCost - warm
	action := "stop"
	if available && expected >= minExpectedSavings {
		action = "warm"
	}
	return Decision{
		Phase: run.phase, WarmCost: warm, MissCost: missCost,
		ContinuationProbability: prob, ExpectedSavings: expected,
		EconomicsAvailable: available, Action: action,
	}
}

func price(tokens int, perMillion float64) float64 {
	return float64(tokens) * perMillion / 1_000_000
}

func (w *Warmer) modeStop(run *active) string {
	switch w.hooks.Mode() {
	case "off":
		return "cache warming disabled"
	case "streaming":
		if run.phase == "idle" {
			return "agent run settled"
		}
	}
	return ""
}

func (w *Warmer) clearLocked() {
	if w.run != nil && w.run.cancel != nil {
		w.run.cancel()
		w.run.gen++
	}
	w.run = nil
}

func (w *Warmer) stopLocked(reason string, decision *Decision, override bool) {
	w.clearLocked()
	w.inactive = Status{State: "inactive", Reason: reason, Decision: decision, ExtensionOverride: override}
}

// FormatStatus is the /session line for a scheduler state.
func FormatStatus(st Status, now time.Time) string {
	d := st.Decision
	if d == nil || (st.State == "inactive" && !d.EconomicsAvailable && !st.ExtensionOverride) {
		reason := st.Reason
		if reason == "" {
			reason = "unknown reason"
		}
		return "Inactive (" + reason + ")"
	}
	details := formatEconomics(*d)
	if st.ExtensionOverride {
		details = "extension override, " + details
	} else {
		details = details + " -> " + d.Action
	}
	switch st.State {
	case "inactive":
		return "Stopped (" + details + ")"
	case "refreshing":
		return "Warming cache (" + details + ")"
	default:
		return formatWhen(st.NextWarmAt, now) + " (" + details + ")"
	}
}

func formatEconomics(d Decision) string {
	if !d.EconomicsAvailable {
		return "cache economics unavailable"
	}
	pct := int(d.ContinuationProbability*100 + 0.5)
	prob := fmt.Sprintf("%d%% continuation probability", pct)
	if d.Phase == "streaming" {
		prob += " while agent is running"
	}
	op := "<"
	if d.Action == "warm" {
		op = ">="
	}
	return fmt.Sprintf("%s, expected savings %s %s $%.3f", prob, dollars(d.ExpectedSavings), op, minExpectedSavings)
}

func dollars(v float64) string {
	if v < 0 {
		return fmt.Sprintf("-$%.3f", -v)
	}
	return fmt.Sprintf("$%.3f", v)
}

func formatWhen(next, now time.Time) string {
	if next.IsZero() || !next.After(now) {
		return "Decision now"
	}
	remain := int((next.Sub(now) + time.Second - 1) / time.Second)
	hours := remain / 3600
	remain %= 3600
	mins := remain / 60
	secs := remain % 60
	var parts []string
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	if secs > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", secs))
	}
	return "Decision in " + strings.Join(parts, " ")
}
