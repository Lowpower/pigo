package ext

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Lowpower/pigo/internal/protocol"
)

// RouteModel is a provider and model id.
type RouteModel struct {
	Provider string
	ID       string
}

// RouteTarget is a physical model and the thinking level dispatched with it.
type RouteTarget struct {
	Model         RouteModel
	ThinkingLevel string
}

// RouteFailure is the failed attempt a retry is replacing.
// Message is the assistant message, which is no longer in Messages.
type RouteFailure struct {
	Model         RouteModel
	ThinkingLevel string
	Message       map[string]any
}

// RouteRequest is one route() call. State is omitted for direct requests.
type RouteRequest struct {
	Model         RouteModel
	ThinkingLevel string
	Reason        string
	Previous      *RouteTarget
	Failed        *RouteFailure
	State         map[string]any
	Messages      []map[string]any
}

// RouteResult is the physical model for this request.
// KeepState, or a nil State, leaves the stored router state unchanged.
// A non-nil State replaces it, including an empty map.
type RouteResult struct {
	Model         RouteModel
	ThinkingLevel string
	State         map[string]any
	KeepState     bool
}

// VirtualModelDef is a selectable model that routes each request.
type VirtualModelDef struct {
	Provider       string
	ID             string
	Name           string
	ThinkingLevels []string
	ContextWindow  int
	MaxTokens      int
	Input          []string
	Route          func(context.Context, RouteRequest) (RouteResult, error)
}

func virtualModelArgs(vm VirtualModelDef) map[string]any {
	levels := vm.ThinkingLevels
	if len(levels) == 0 {
		levels = []string{"off"}
	}
	input := vm.Input
	if len(input) == 0 {
		input = []string{"text", "image"}
	}
	args := map[string]any{
		"provider":       vm.Provider,
		"id":             vm.ID,
		"thinkingLevels": levels,
		"input":          input,
	}
	if vm.Name != "" {
		args["name"] = vm.Name
	}
	if vm.ContextWindow > 0 {
		args["contextWindow"] = vm.ContextWindow
	}
	if vm.MaxTokens > 0 {
		args["maxTokens"] = vm.MaxTokens
	}
	return args
}

func virtualModelFromArgs(args map[string]any) (VirtualModelDef, error) {
	if args == nil {
		return VirtualModelDef{}, errors.New("missing virtual model")
	}
	vm := VirtualModelDef{
		Provider:       stringField(args, "provider"),
		ID:             stringField(args, "id"),
		Name:           stringField(args, "name"),
		ThinkingLevels: stringList(args["thinkingLevels"]),
		Input:          stringList(args["input"]),
		ContextWindow:  intField(args["contextWindow"]),
		MaxTokens:      intField(args["maxTokens"]),
	}
	if vm.Provider == "" || vm.ID == "" {
		return VirtualModelDef{}, errors.New("virtual model requires provider and id")
	}
	if len(vm.ThinkingLevels) == 0 {
		vm.ThinkingLevels = []string{"off"}
	}
	if len(vm.Input) == 0 {
		vm.Input = []string{"text", "image"}
	}
	return vm, nil
}

func callRoute(ctx context.Context, fn func(context.Context, RouteRequest) (RouteResult, error), req RouteRequest) (res RouteResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("route panicked: %v", rec)
		}
	}()
	if fn == nil {
		return RouteResult{}, errors.New("no route handler")
	}
	return fn(ctx, req)
}

func routeRequestFromPayload(payload map[string]any) RouteRequest {
	if payload == nil {
		return RouteRequest{}
	}
	req := RouteRequest{
		Model:         routeModelField(payload["model"]),
		ThinkingLevel: stringField(payload, "thinkingLevel"),
		Reason:        stringField(payload, "reason"),
		Messages:      mapList(payload["messages"]),
	}
	if req.Model.Provider == "" {
		req.Model.Provider = stringField(payload, "provider")
	}
	if req.Model.ID == "" {
		req.Model.ID = stringField(payload, "id")
	}
	if prev, ok := payload["previous"].(map[string]any); ok {
		req.Previous = &RouteTarget{
			Model:         routeModelField(prev["model"]),
			ThinkingLevel: stringField(prev, "thinkingLevel"),
		}
	}
	if failed, ok := payload["failed"].(map[string]any); ok {
		msg, _ := failed["message"].(map[string]any)
		req.Failed = &RouteFailure{
			Model:         routeModelField(failed["model"]),
			ThinkingLevel: stringField(failed, "thinkingLevel"),
			Message:       msg,
		}
	}
	if state, ok := payload["state"].(map[string]any); ok {
		req.State = state
	}
	return req
}

func routeResultPayload(req RouteRequest, res RouteResult) map[string]any {
	out := map[string]any{
		"model": map[string]any{
			"provider": res.Model.Provider,
			"id":       res.Model.ID,
		},
		"thinkingLevel": res.ThinkingLevel,
	}
	if !res.KeepState && res.State != nil && req.Reason != "direct" {
		out["state"] = res.State
	}
	return out
}

func routeModelField(v any) RouteModel {
	m, ok := v.(map[string]any)
	if !ok {
		return RouteModel{}
	}
	return RouteModel{Provider: stringField(m, "provider"), ID: stringField(m, "id")}
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func stringList(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func intField(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func mapList(v any) []map[string]any {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if ok {
			out = append(out, m)
		}
	}
	return out
}

// SetVirtualModelHook receives virtual models registered after the handshake.
// drop is true for unregister_virtual_model.
func (h *Host) SetVirtualModelHook(fn func(VirtualModelDef, bool)) {
	h.mu.Lock()
	h.virtualHook = fn
	h.mu.Unlock()
}

// VirtualModels returns virtual models this extension registered.
func (h *Host) VirtualModels() []VirtualModelDef {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]VirtualModelDef(nil), h.virtuals...)
}

// Route asks the extension to pick a physical model. The caller context
// cancels the wait and aborts the extension's route handler.
func (h *Host) Route(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id := newID()
	ch := make(chan protocol.Message, 1)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, errClosed
	}
	h.pending[id] = ch
	h.mu.Unlock()

	if err := h.send(protocol.Message{Type: protocol.TypeRoute, ID: id, Payload: payload}); err != nil {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	select {
	case got, ok := <-ch:
		if !ok {
			return nil, errClosed
		}
		if got.IsError {
			msg := got.Result
			if msg == "" {
				msg = "route failed"
			}
			return nil, errors.New(msg)
		}
		if got.Payload == nil {
			return map[string]any{}, nil
		}
		return got.Payload, nil
	case <-callCtx.Done():
		_ = h.send(protocol.Message{Type: protocol.TypeRouteAbort, ID: id})
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return nil, callCtx.Err()
	}
}

func (h *Host) upsertVirtual(def VirtualModelDef) {
	for i, cur := range h.virtuals {
		if cur.Provider == def.Provider && cur.ID == def.ID {
			h.virtuals[i] = def
			return
		}
	}
	h.virtuals = append(h.virtuals, def)
}

func (h *Host) removeVirtual(provider, id string) {
	out := make([]VirtualModelDef, 0, len(h.virtuals))
	for _, cur := range h.virtuals {
		if cur.Provider == provider && cur.ID == id {
			continue
		}
		out = append(out, cur)
	}
	h.virtuals = out
}
