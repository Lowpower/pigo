package runtime

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/ext"
)

// Limits match pi v1.3.0 nested-call records.
const (
	maxNestedCalls         = 256
	maxNestedArgBytes      = 8 * 1024
	maxNestedArgBytesTotal = 32 * 1024
	maxNestedErrorChars    = 500
)

type nestedScope struct {
	rec  *nestedRecorder
	next int
	ctx  context.Context
}

type nestedSnapshot struct {
	calls *ai.NestedToolCalls
	usage *ai.Usage
}

type nestedRecorder struct {
	mu       sync.Mutex
	calls    []ai.NestedToolCall
	started  map[string]time.Time
	complete bool
	argBytes int
	usage    *ai.Usage
}

func newNestedRecorder() *nestedRecorder {
	return &nestedRecorder{complete: true, started: map[string]time.Time{}}
}

func (r *nestedRecorder) tryStart(id, name string, args map[string]any) bool {
	raw, err := json.Marshal(args)
	if err != nil || args == nil {
		raw = []byte("{}")
	}
	n := len(raw)
	rec := ai.NestedToolCall{ID: id, Name: name, Status: "unfinished"}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) >= maxNestedCalls {
		r.complete = false
		return false
	}
	if n > maxNestedArgBytes || r.argBytes+n > maxNestedArgBytesTotal {
		rec.ArgumentsBytes = n
		r.complete = false
	} else {
		var parsed map[string]any
		if json.Unmarshal(raw, &parsed) == nil {
			rec.Arguments = parsed
		}
		r.argBytes += n
	}
	r.calls = append(r.calls, rec)
	r.started[id] = time.Now()
	return true
}

func (r *nestedRecorder) finish(id string, isErr bool, errText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.calls {
		if r.calls[i].ID != id || r.calls[i].Status != "unfinished" {
			continue
		}
		if isErr {
			r.calls[i].Status = "error"
			if errText != "" {
				r.calls[i].Error = trimRunes(errText, maxNestedErrorChars)
			}
		} else {
			r.calls[i].Status = "ok"
		}
		if started, ok := r.started[id]; ok {
			r.calls[i].DurationMs = int(time.Since(started) / time.Millisecond)
			delete(r.started, id)
		}
		return
	}
}

func (r *nestedRecorder) addUsage(u ai.Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.usage == nil {
		r.usage = &u
		return
	}
	sum := ai.AddUsage(*r.usage, u)
	r.usage = &sum
}

func (r *nestedRecorder) snapshot() nestedSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	var usage *ai.Usage
	if r.usage != nil {
		u := *r.usage
		usage = &u
	}
	if len(r.calls) == 0 && r.complete {
		return nestedSnapshot{usage: usage}
	}
	calls := append([]ai.NestedToolCall(nil), r.calls...)
	complete := r.complete
	for _, call := range calls {
		if call.Status == "unfinished" {
			complete = false
			break
		}
	}
	return nestedSnapshot{
		calls: &ai.NestedToolCalls{Calls: calls, Complete: complete},
		usage: usage,
	}
}

func trimRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for count := 0; i < len(s) && count < n; count++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

func (e *Engine) openNested(ctx context.Context, id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.nested == nil {
		e.nested = map[string]*nestedScope{}
	}
	e.nested[id] = &nestedScope{rec: newNestedRecorder(), ctx: ctx}
}

func (e *Engine) finishNested(id string) nestedSnapshot {
	e.mu.Lock()
	scope := e.nested[id]
	delete(e.nested, id)
	e.mu.Unlock()
	if scope == nil || scope.rec == nil {
		return nestedSnapshot{}
	}
	return scope.rec.snapshot()
}

func (e *Engine) bindToolEmit(ctx context.Context) {
	fn := agent.ToolEmitter(ctx)
	if fn == nil {
		return
	}
	e.mu.Lock()
	e.toolEmit = fn
	e.mu.Unlock()
}

func (e *Engine) emitToolEvent(ev agent.Event) {
	e.mu.Lock()
	fn := e.toolEmit
	e.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

func (e *Engine) executeNested(ctx context.Context, parent, name string, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	e.mu.Lock()
	scope := e.nested[parent]
	if scope == nil || scope.rec == nil {
		e.mu.Unlock()
		return map[string]any{"content": "executeTool requires an active parent tool call", "isError": true}
	}
	scope.next++
	id := parent + "/" + strconv.Itoa(scope.next)
	rec := scope.rec
	runCtx := scope.ctx
	if e.nested == nil {
		e.nested = map[string]*nestedScope{}
	}
	e.nested[id] = &nestedScope{rec: rec, ctx: runCtx}
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.nested, id)
		e.mu.Unlock()
	}()
	if runCtx == nil {
		runCtx = ctx
	}
	if runCtx == nil {
		runCtx = context.Background()
	}
	if !rec.tryStart(id, name, args) {
		return map[string]any{"content": "nested tool call limit exceeded", "isError": true, "toolCallId": id}
	}
	e.emitToolEvent(agent.Event{
		Type: agent.EventToolStart, ToolCallID: id, ToolName: name, Args: args, ParentToolCallID: parent,
	})
	e.DispatchEvent(runCtx, "tool_execution_start", map[string]any{
		"toolCallId": id, "toolName": name, "input": args, "parentToolCallId": parent,
	})
	out, isErr, structured, usage := e.runNestedTool(runCtx, id, name, args)
	if usage != nil {
		rec.addUsage(*usage)
	}
	errText := ""
	if isErr {
		errText = out
	}
	rec.finish(id, isErr, errText)
	e.emitToolEvent(agent.Event{
		Type: agent.EventToolEnd, ToolCallID: id, ToolName: name, Result: out, IsError: isErr, ParentToolCallID: parent,
	})
	e.DispatchEvent(runCtx, "tool_execution_end", map[string]any{
		"toolCallId": id, "toolName": name, "content": out, "isError": isErr, "parentToolCallId": parent,
	})
	res := map[string]any{"content": out, "isError": isErr, "toolCallId": id}
	if len(structured) > 0 {
		var v any
		if json.Unmarshal(structured, &v) == nil {
			res["structuredContent"] = v
		}
	}
	if usage != nil {
		res["usage"] = usage
	}
	return res
}

func (e *Engine) runNestedTool(ctx context.Context, id, name string, args map[string]any) (string, bool, json.RawMessage, *ai.Usage) {
	if info, ok := e.toolInfo(name); ok && !ext.Callable(info.Exposure) {
		return "tool is not callable", true, nil, nil
	}
	if e.Tools == nil || !e.Tools.Has(name) {
		return "unknown tool: " + name, true, nil, nil
	}
	box := &ext.ToolResult{}
	callCtx := ext.WithOutcome(ext.WithToolCallID(ctx, id), box)
	out, isErr := e.Tools.Execute(callCtx, name, args)
	return out, isErr, box.Structured, box.Usage
}
