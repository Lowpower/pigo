package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/cachewarm"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/session"
)

func (e *Engine) cacheWarmingMode() string {
	if e.Opts.UserConfig != nil {
		return e.Opts.UserConfig.CacheWarmingMode()
	}
	return e.Opts.Config.CacheWarmingMode()
}

func (e *Engine) newCacheWarmer() *cachewarm.Warmer {
	return cachewarm.New(cachewarm.Hooks{
		Mode:    e.cacheWarmingMode,
		Current: e.warmContextCurrent,
		Decide:  e.decideCacheWarm,
		Refresh: e.refreshCache,
		Record:  e.recordCacheWarm,
		PromptTokens: func() int {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.warmTokens
		},
	})
}

func (e *Engine) decideCacheWarm(d cachewarm.Decision) (string, error) {
	res := e.DispatchEvent(context.Background(), "cache_warming_decision", map[string]any{
		"warmCost":                d.WarmCost,
		"missCost":                d.MissCost,
		"continuationProbability": d.ContinuationProbability,
		"action":                  d.Action,
	})
	if action, ok := res["action"].(string); ok {
		return action, nil
	}
	return d.Action, nil
}

func (e *Engine) refreshCache(ctx context.Context, req cachewarm.Request) (ai.Usage, string, error) {
	if e.Stream == nil {
		return ai.Usage{}, string(ai.StopError), nil
	}
	opts := req.Options
	opts.MaxTokens = 1
	stream, err := e.Stream(ctx, req.Context, opts)
	if err != nil || stream == nil {
		if err == nil {
			err = context.Canceled
		}
		return ai.Usage{}, string(ai.StopError), err
	}
	_, msg := stream.Collect()
	if msg == nil {
		return ai.Usage{}, string(ai.StopError), nil
	}
	return msg.Usage, string(msg.StopReason), nil
}

func (e *Engine) recordCacheWarm(usage ai.Usage, provider, model, note string) {
	if e.Opts.Session == nil {
		return
	}
	_, _ = e.Opts.Session.AppendUsage("cache_warm", provider, model, usage, note)
}

func (e *Engine) warmContextCurrent() bool {
	e.mu.Lock()
	key := e.warmKey
	base := append([]ai.Message(nil), e.warmReq...)
	e.mu.Unlock()
	if key == "" {
		return true
	}
	if e.Provider+"/"+e.Opts.Config.ResolvedModel() != key {
		return false
	}
	if e.Opts.Session == nil {
		return true
	}
	cur := session.RestoreAIMessages(session.ContextEntries(e.Opts.Session))
	return messagesPrefix(base, cur)
}

func (e *Engine) observeCacheWarm(ctx context.Context, stream *ai.EventStream, req ai.Context, opts ai.Options) *ai.EventStream {
	out := ai.NewEventStream(8)
	go func() {
		defer out.Close()
		var final *ai.AssistantMessage
		ok := false
		for ev := range stream.Events() {
			if ev.Type == ai.EventDone {
				final = ev.Message
				ok = final != nil && final.StopReason != ai.StopError && final.StopReason != ai.StopAborted
			}
			if !out.Push(ctx, ev) {
				if ok {
					e.noteCacheRequest(req, opts, final)
				}
				return
			}
		}
		if ok {
			e.noteCacheRequest(req, opts, final)
		}
	}()
	return out
}

func (e *Engine) noteCacheRequest(req ai.Context, opts ai.Options, msg *ai.AssistantMessage) {
	if e.warmer == nil || msg == nil {
		return
	}
	provider := opts.Provider
	if provider == "" {
		provider = e.Provider
	}
	model, ok := models.Lookup(provider, opts.Model)
	if !ok {
		model = models.Model{Provider: provider, ID: opts.Model, API: models.APIFor(provider, opts.Model)}
	}
	adaptive := model.Compat != nil && model.Compat.SupportsMidConvoEffort
	tokens := msg.Usage.Input + msg.Usage.CacheRead + msg.Usage.CacheWrite
	e.mu.Lock()
	e.warmReq = append([]ai.Message(nil), req.Messages...)
	e.warmKey = provider + "/" + opts.Model
	e.warmTokens = tokens
	e.mu.Unlock()
	opts.Provider = provider
	e.warmer.Start(cachewarm.Request{
		Model: model, Context: req, Options: opts, PromptTokens: tokens, Adaptive: adaptive,
	})
}

// CacheWarmingStatus is the one-line /session diagnostic.
func (e *Engine) CacheWarmingStatus() string {
	if e.warmer == nil {
		return "Inactive (waiting for first request)"
	}
	return cachewarm.FormatStatus(e.warmer.Status(), time.Now())
}

// CacheWarmingChanged re-reads the mode after a settings change.
func (e *Engine) CacheWarmingChanged() {
	if e.warmer != nil {
		e.warmer.ModeChanged()
	}
}

func messagesPrefix(base, cur []ai.Message) bool {
	if len(cur) < len(base) {
		return false
	}
	for i := range base {
		if base[i].Role != cur[i].Role || strings.TrimSpace(base[i].Content) != strings.TrimSpace(cur[i].Content) {
			return false
		}
	}
	return true
}
