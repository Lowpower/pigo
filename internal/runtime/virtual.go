package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/cachewarm"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
)

const (
	routeReasonUser         = "user"
	routeReasonContinuation = "continuation"
	routeReasonRetry        = "retry"
	routeReasonDirect       = "direct"
)

type routeReasonKey struct{}

func withRouteReason(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, routeReasonKey{}, reason)
}

func reasonFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	s, ok := ctx.Value(routeReasonKey{}).(string)
	return s, ok && s != ""
}

// virtualModel is one extension-registered selectable model.
type virtualModel struct {
	Provider       string
	ID             string
	Name           string
	ThinkingLevels []string
	ContextWindow  int
	MaxTokens      int
	Input          []string
	Host           *ext.Host
}

func (vm virtualModel) key() string { return vm.Provider + "\x00" + vm.ID }

func (vm virtualModel) model() models.Model {
	levels := vm.ThinkingLevels
	if len(levels) == 0 {
		levels = []string{"off"}
	}
	allow := map[string]bool{}
	for _, level := range levels {
		allow[strings.ToLower(strings.TrimSpace(level))] = true
	}
	thinking := make(map[string]*string, len(models.ThinkingLevels))
	for _, level := range models.ThinkingLevels {
		if !allow[level] {
			thinking[level] = nil
			continue
		}
		value := level
		thinking[level] = &value
	}
	input := vm.Input
	if len(input) == 0 {
		input = []string{"text", "image"}
	}
	return models.Model{
		Provider:         vm.Provider,
		ID:               vm.ID,
		Name:             vm.Name,
		ContextWindow:    vm.ContextWindow,
		MaxTokens:        vm.MaxTokens,
		Input:            append([]string(nil), input...),
		ThinkingLevelMap: thinking,
	}
}

func (e *Engine) applyVirtualModels() {
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		host := h
		h.SetVirtualModelHook(func(def ext.VirtualModelDef, drop bool) {
			if drop {
				e.dropVirtual(host, def.Provider, def.ID)
				return
			}
			e.installVirtual(host, def)
		})
		for _, vm := range h.VirtualModels() {
			e.installVirtual(h, vm)
		}
	}
}

func (e *Engine) dropAllVirtuals() {
	e.mu.Lock()
	e.virtuals = nil
	e.mu.Unlock()
	e.refreshScoped()
}

func (e *Engine) installVirtual(h *ext.Host, def ext.VirtualModelDef) {
	if def.Provider == "" || def.ID == "" {
		return
	}
	vm := virtualModel{
		Provider:       def.Provider,
		ID:             def.ID,
		Name:           def.Name,
		ThinkingLevels: append([]string(nil), def.ThinkingLevels...),
		ContextWindow:  def.ContextWindow,
		MaxTokens:      def.MaxTokens,
		Input:          append([]string(nil), def.Input...),
		Host:           h,
	}
	key := vm.key()
	e.mu.Lock()
	_, replacing := e.virtuals[key]
	e.mu.Unlock()
	if !replacing {
		if _, ok := models.Lookup(def.Provider, def.ID); ok {
			fmt.Fprintf(os.Stderr, "pigo: virtual model %s/%s matches a physical model and was not registered\n", def.Provider, def.ID)
			return
		}
	}
	e.mu.Lock()
	if e.virtuals == nil {
		e.virtuals = map[string]virtualModel{}
	}
	e.virtuals[key] = vm
	e.mu.Unlock()
	e.refreshScoped()
	e.wrapVirtualRoute()
}

func (e *Engine) dropVirtual(h *ext.Host, provider, id string) {
	key := provider + "\x00" + id
	e.mu.Lock()
	cur, ok := e.virtuals[key]
	if ok && (h == nil || cur.Host == h) {
		delete(e.virtuals, key)
	}
	e.mu.Unlock()
	e.refreshScoped()
}

func (e *Engine) lookupVirtual(provider, id string) (virtualModel, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	vm, ok := e.virtuals[provider+"\x00"+id]
	return vm, ok
}

func (e *Engine) virtualList() []virtualModel {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]virtualModel, 0, len(e.virtuals))
	for _, vm := range e.virtuals {
		out = append(out, vm)
	}
	return out
}

func (e *Engine) hasCredentials(provider string) bool {
	return auth.CheckAuth(auth.Open(e.Opts.AgentDir), provider) != nil
}

func (e *Engine) virtualSelectable(vm virtualModel) bool {
	if _, ok := models.LookupProvider(vm.Provider); !ok {
		return true
	}
	return e.hasCredentials(vm.Provider)
}

// VisibleModels is the model list shown by /model and settings.
func (e *Engine) VisibleModels() []models.Model {
	if e.Opts.AgentDir != "" {
		ids := auth.AuthenticatedIDs(auth.Open(e.Opts.AgentDir))
		if list := models.Available(ids); len(list) > 0 {
			return e.mergeVirtual(list)
		}
	}
	return e.mergeVirtual(models.Catalog())
}

func (e *Engine) mergeVirtual(base []models.Model) []models.Model {
	virt := e.virtualList()
	if len(virt) == 0 {
		return base
	}
	hide := map[string]bool{}
	var extra []models.Model
	for _, vm := range virt {
		hide[vm.Provider+"/"+vm.ID] = true
		if e.virtualSelectable(vm) {
			extra = append(extra, vm.model())
		}
	}
	out := make([]models.Model, 0, len(base)+len(extra))
	for _, m := range base {
		if hide[m.Provider+"/"+m.ID] {
			continue
		}
		out = append(out, m)
	}
	return append(out, extra...)
}

func (e *Engine) refreshScoped() {
	patterns, ok := e.scopedPatterns()
	if !ok {
		return
	}
	e.Scoped = models.ResolvePatternsIn(patterns, e.VisibleModels())
}

func (e *Engine) scopedPatterns() ([]string, bool) {
	if len(e.Opts.Models) > 0 {
		return e.Opts.Models, true
	}
	if e.Opts.UserConfig != nil && e.Opts.UserConfig.EnabledModels != nil {
		return e.Opts.UserConfig.EnabledModels, true
	}
	if e.Opts.Config.EnabledModels != nil {
		return e.Opts.Config.EnabledModels, true
	}
	return nil, false
}

// directStream marks a provider call as outside the agent loop.
func (e *Engine) directStream() ai.StreamFn {
	return func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		if e.Stream == nil {
			return nil, errors.New("no stream")
		}
		return e.Stream(withRouteReason(ctx, routeReasonDirect), req, opts)
	}
}

func (e *Engine) armRouteRetry(msg *ai.AssistantMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pendingRouteReason = routeReasonRetry
	e.pendingFailed = cloneAssistant(msg)
}

func (e *Engine) clearArmedRetry() {
	e.mu.Lock()
	e.pendingRouteReason = ""
	e.pendingFailed = nil
	e.mu.Unlock()
}

func (e *Engine) takeArmedRetry() (string, *ai.AssistantMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	reason := e.pendingRouteReason
	failed := e.pendingFailed
	e.pendingRouteReason = ""
	e.pendingFailed = nil
	return reason, failed
}

func (e *Engine) wrapVirtualRoute() {
	e.mu.Lock()
	if e.routerOn || len(e.virtuals) == 0 {
		e.mu.Unlock()
		return
	}
	inner := e.Stream
	e.baseStream = inner
	e.routerOn = true
	e.mu.Unlock()
	e.Stream = func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		e.mu.Lock()
		base := e.baseStream
		e.mu.Unlock()
		if base == nil {
			base = inner
		}
		return e.dispatchStream(ctx, req, opts, base)
	}
}

func (e *Engine) assignBaseStream(fn ai.StreamFn) {
	if fn == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.routerOn {
		e.baseStream = fn
		return
	}
	e.Stream = fn
}

func (e *Engine) unroute() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.routerOn && e.baseStream != nil {
		e.Stream = e.baseStream
	}
	e.routerOn = false
	e.baseStream = nil
}

func (e *Engine) dispatchStream(ctx context.Context, req ai.Context, opts ai.Options, inner ai.StreamFn) (*ai.EventStream, error) {
	if cachewarm.IsRefresh(ctx) {
		return e.callProviderStream(ctx, req, opts, inner)
	}
	provider := opts.Provider
	if provider == "" {
		provider = e.activeProvider()
	}
	id := opts.Model
	if id == "" {
		id = e.Opts.Config.ResolvedModel()
	}
	vm, ok := e.lookupVirtual(provider, id)
	if !ok {
		if !cachewarm.IsRefresh(ctx) {
			e.clearArmedRetry()
		}
		if inner == nil {
			return nil, errors.New("no stream")
		}
		return inner(ctx, req, opts)
	}
	dispatched, err := e.routeVirtual(ctx, vm, req, opts)
	if err != nil {
		return nil, err
	}
	if e.Opts.Config.BlockImages() {
		req.Messages = ai.BlockImages(req.Messages)
	}
	opts.Provider = dispatched.Provider
	opts.Model = dispatched.ID
	opts.Thinking = dispatched.Thinking
	stream, err := e.callProviderStream(ctx, req, opts, nil)
	if err != nil || stream == nil {
		return stream, err
	}
	return stampDispatch(ctx, stream, dispatched.Provider, dispatched.ID, dispatched.Thinking), nil
}

type dispatchedModel struct {
	Provider string
	ID       string
	Thinking string
}

func (e *Engine) routeVirtual(ctx context.Context, vm virtualModel, req ai.Context, opts ai.Options) (dispatchedModel, error) {
	label := vm.Provider + "/" + vm.ID
	if vm.Host == nil {
		return dispatchedModel{}, fmt.Errorf("virtual model %s: no extension", label)
	}
	reason, failed := e.routeReason(ctx, req)
	payload := e.routePayload(vm, reason, failed, req, opts)
	res, err := vm.Host.Route(ctx, payload)
	if err != nil {
		return dispatchedModel{}, fmt.Errorf("virtual model %s: %w", label, err)
	}
	provider, id := routeResultModel(res)
	if provider == "" || id == "" {
		return dispatchedModel{}, fmt.Errorf("virtual model %s: route returned no model", label)
	}
	if _, virt := e.lookupVirtual(provider, id); virt {
		return dispatchedModel{}, fmt.Errorf("virtual model %s: route returned virtual model %s/%s", label, provider, id)
	}
	phys, ok := models.Lookup(provider, id)
	if !ok {
		return dispatchedModel{}, fmt.Errorf("virtual model %s: unknown model %s/%s", label, provider, id)
	}
	if !e.hasCredentials(provider) {
		return dispatchedModel{}, fmt.Errorf("virtual model %s: %s has no credentials", label, provider)
	}
	thinking := strings.ToLower(strings.TrimSpace(asString(res["thinkingLevel"])))
	if thinking == "" {
		thinking = "off"
	}
	thinking = models.ClampThinking(thinking, phys)
	if reason != routeReasonDirect {
		if state, ok := res["state"]; ok {
			e.storeRouteState(cloneJSONMap(state))
		}
	}
	return dispatchedModel{Provider: provider, ID: id, Thinking: thinking}, nil
}

func (e *Engine) routeReason(ctx context.Context, req ai.Context) (string, *ai.AssistantMessage) {
	if reason, ok := reasonFromContext(ctx); ok {
		return reason, nil
	}
	if reason, failed := e.takeArmedRetry(); reason == routeReasonRetry {
		return reason, failed
	}
	return inferRouteReason(req.Messages), nil
}

func inferRouteReason(msgs []ai.Message) string {
	if len(msgs) == 0 || msgs[len(msgs)-1].Role == ai.RoleUser {
		return routeReasonUser
	}
	return routeReasonContinuation
}

func (e *Engine) routePayload(vm virtualModel, reason string, failed *ai.AssistantMessage, req ai.Context, opts ai.Options) map[string]any {
	thinking := opts.Thinking
	if thinking == "" {
		thinking = e.Opts.Config.Thinking
	}
	messages := make([]ai.Message, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, ai.Message{Role: ai.RoleSystem, Content: req.System})
	}
	messages = append(messages, req.Messages...)
	rawMsgs := make([]any, 0, len(messages))
	for _, m := range messages {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		var item any
		if json.Unmarshal(b, &item) == nil {
			rawMsgs = append(rawMsgs, item)
		}
	}
	payload := map[string]any{
		"provider":      vm.Provider,
		"id":            vm.ID,
		"model":         map[string]any{"provider": vm.Provider, "id": vm.ID},
		"thinkingLevel": thinking,
		"reason":        reason,
		"messages":      rawMsgs,
	}
	if prev, ok := previousDispatch(req.Messages); ok {
		payload["previous"] = map[string]any{
			"model":         map[string]any{"provider": prev.Provider, "id": prev.Model},
			"thinkingLevel": prev.ProviderThinkingLevel,
		}
	}
	if reason == routeReasonRetry && failed != nil {
		var msg any
		if b, err := json.Marshal(failed); err == nil {
			_ = json.Unmarshal(b, &msg)
		}
		payload["failed"] = map[string]any{
			"model":         map[string]any{"provider": failed.Provider, "id": failed.Model},
			"thinkingLevel": failed.ProviderThinkingLevel,
			"message":       msg,
		}
	}
	if reason != routeReasonDirect {
		if state := e.routeState(); state != nil {
			payload["state"] = state
		}
	}
	return payload
}

func previousDispatch(msgs []ai.Message) (ai.AssistantMessage, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i].Assistant
		if msg == nil {
			continue
		}
		switch msg.StopReason {
		case ai.StopError, ai.StopAborted, ai.StopPending, "":
			continue
		}
		if msg.Provider == "" && msg.Model == "" {
			continue
		}
		return *msg, true
	}
	return ai.AssistantMessage{}, false
}

func routeResultModel(res map[string]any) (provider, id string) {
	if res == nil {
		return "", ""
	}
	if m, ok := res["model"].(map[string]any); ok {
		provider = asString(m["provider"])
		id = asString(m["id"])
	}
	if provider == "" {
		provider = asString(res["provider"])
	}
	if id == "" {
		id = asString(res["id"])
	}
	return provider, id
}

func (e *Engine) routeBranchKey() string {
	if e.Opts.Session == nil {
		return ""
	}
	return e.Opts.Session.ID() + "\x00" + e.Opts.Session.LeafID()
}

func (e *Engine) routeState() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	cur := e.routeStates[e.routeBranchKey()]
	if cur == nil {
		return nil
	}
	return cloneJSONMap(cur)
}

func (e *Engine) storeRouteState(state map[string]any) {
	if state == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.routeStates == nil {
		e.routeStates = map[string]map[string]any{}
	}
	e.routeStates[e.routeBranchKey()] = state
}

func (e *Engine) callProviderStream(ctx context.Context, req ai.Context, opts ai.Options, fallback ai.StreamFn) (*ai.EventStream, error) {
	provider := opts.Provider
	if provider == "" {
		provider = e.activeProvider()
	}
	if fn := e.bindStream(provider); fn != nil {
		return fn(ctx, req, opts)
	}
	if fallback != nil {
		return fallback(ctx, req, opts)
	}
	return nil, fmt.Errorf("no stream for %s", provider)
}

func stampDispatch(ctx context.Context, stream *ai.EventStream, provider, model, thinking string) *ai.EventStream {
	out := ai.NewEventStream(8)
	go func() {
		defer out.Close()
		for ev := range stream.Events() {
			stampAssistant(ev.Partial, provider, model, thinking)
			stampAssistant(ev.Message, provider, model, thinking)
			if !out.Push(ctx, ev) {
				return
			}
		}
	}()
	return out
}

func stampAssistant(msg *ai.AssistantMessage, provider, model, thinking string) {
	if msg == nil {
		return
	}
	if msg.Provider == "" {
		msg.Provider = provider
	}
	if msg.Model == "" {
		msg.Model = model
	}
	if msg.ProviderThinkingLevel == "" && thinking != "" {
		msg.ProviderThinkingLevel = thinking
	}
}

func cloneAssistant(msg *ai.AssistantMessage) *ai.AssistantMessage {
	if msg == nil {
		return nil
	}
	b, err := json.Marshal(msg)
	if err != nil {
		cp := *msg
		return &cp
	}
	var out ai.AssistantMessage
	if json.Unmarshal(b, &out) != nil {
		cp := *msg
		return &cp
	}
	return &out
}

func cloneJSONMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}
