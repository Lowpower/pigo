package tools

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/invopop/jsonschema"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

// Tool is one built-in tool: a name, a description, a JSON-Schema for its
// parameters, and an execution function. Execute returns the textual result and
// whether it represents an error (matching the agent's ToolExecutor contract).
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, args map[string]any) (result string, isError bool)
}

// Registry holds a set of tools in a stable order.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Tool
	order  []Tool
}

// NewRegistry builds a registry from the given tools (later duplicates win).
func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{byName: make(map[string]Tool)}
	for _, t := range ts {
		if _, ok := r.byName[t.Name()]; !ok {
			r.order = append(r.order, t)
		}
		r.byName[t.Name()] = t
	}
	return r
}

// Options configures built-in tool runtime behaviour.
type Options struct {
	AutoResize   bool
	ImageResize  func() *models.ImageResize
	ShellPrefix  string
	Cwd          string
	Env          map[string]string
	EnvFn        func() map[string]string
	ImageCapable func() bool
	Runner       Runner
}

// Default returns a registry with the built-in tools. powershell is Windows-only.
func Default() *Registry {
	return NewBuiltins(Options{AutoResize: true})
}

// NewBuiltins builds the built-in tool set with runtime options.
func NewBuiltins(opt Options) *Registry {
	r := useRunner(opt.Runner)
	ts := []Tool{
		readTool{autoResize: opt.AutoResize, resize: opt.ImageResize, cwd: opt.Cwd, imageCapable: opt.ImageCapable, fs: r},
		writeTool{cwd: opt.Cwd, fs: r},
		editTool{cwd: opt.Cwd, fs: r},
		bashTool{prefix: opt.ShellPrefix, cwd: opt.Cwd, env: opt.Env, envFn: opt.EnvFn, fs: r},
		grepTool{cwd: opt.Cwd, fs: r},
		findTool{cwd: opt.Cwd, fs: r},
		listTool{cwd: opt.Cwd, fs: r},
	}
	ts = append(ts, extraPlatformTools(opt)...)
	return NewRegistry(ts...)
}

// AITools returns the tools as provider-neutral ai.Tool definitions.
func (r *Registry) AITools() []ai.Tool {
	return defsOf(r.List())
}

// Has reports whether a tool with this name is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.get(name)
	return ok
}

// AIToolsNamed returns registered tools whose names are in names, in registry
// order. A nil names slice means all tools.
func (r *Registry) AIToolsNamed(names []string) []ai.Tool {
	all := r.List()
	if names == nil {
		return defsOf(all)
	}
	allow := make(map[string]bool, len(names))
	for _, n := range names {
		allow[n] = true
	}
	kept := make([]Tool, 0, len(names))
	for _, t := range all {
		if allow[t.Name()] {
			kept = append(kept, t)
		}
	}
	return defsOf(kept)
}

// List returns tools in registration order.
func (r *Registry) List() []Tool {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, len(r.order))
	copy(out, r.order)
	return out
}

// Upsert inserts or replaces a tool by name. An existing name keeps its place.
func (r *Registry) Upsert(tool Tool) {
	if r == nil || tool == nil || tool.Name() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byName == nil {
		r.byName = map[string]Tool{}
	}
	name := tool.Name()
	if _, ok := r.byName[name]; !ok {
		r.order = append(r.order, tool)
	} else {
		for i, existing := range r.order {
			if existing.Name() == name {
				r.order[i] = tool
				break
			}
		}
	}
	r.byName[name] = tool
}

// Drop removes tools whose names are set in names.
func (r *Registry) Drop(names map[string]bool) {
	if r == nil || len(names) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := make([]Tool, 0, len(r.order))
	for _, t := range r.order {
		if names[t.Name()] {
			delete(r.byName, t.Name())
			continue
		}
		next = append(next, t)
	}
	r.order = next
}

func (r *Registry) get(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byName[name]
	return t, ok
}

func defsOf(list []Tool) []ai.Tool {
	out := make([]ai.Tool, 0, len(list))
	for _, t := range list {
		out = append(out, ai.Tool{
			Name:                t.Name(),
			Description:         t.Description(),
			Parameters:          t.Schema(),
			ConstrainedSampling: preferConstrainedSampling(t.Name()),
		})
	}
	return out
}

// ReadyCall is a tool invocation that passed name lookup and argument checks.
type ReadyCall struct {
	tool Tool
	args map[string]any
}

// Execute runs the tool body.
func (c ReadyCall) Execute(ctx context.Context) (string, bool) {
	if c.tool == nil {
		return "", true
	}
	return c.tool.Execute(ctx, c.args)
}

// Ready prepares a call. ok is false when the tool is unknown or the arguments
// are invalid; result is the error text and the tool body has not run.
func (r *Registry) Ready(name string, args map[string]any) (call ReadyCall, result string, isError, ok bool) {
	if r == nil {
		return ReadyCall{}, "unknown tool: " + name, true, false
	}
	t, found := r.get(name)
	if !found {
		return ReadyCall{}, "unknown tool: " + name, true, false
	}
	prepared, err := prepareToolArgs(t, args)
	if err != nil {
		return ReadyCall{}, "invalid arguments: " + err.Error(), true, false
	}
	return ReadyCall{tool: t, args: prepared}, "", false, true
}

// Execute dispatches a tool call by name. It satisfies the shape expected by an
// agent ToolExecutor adapter: Execute(ctx, name, args).
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, bool) {
	call, result, isErr, ok := r.Ready(name, args)
	if !ok {
		return result, isErr
	}
	return call.Execute(ctx)
}

// schemaFor reflects a JSON Schema from a params struct, inlined (no $ref) and
// stripped of metadata keys so it can be used directly as a tool input schema.
func schemaFor(v any) map[string]any {
	reflector := &jsonschema.Reflector{
		ExpandedStruct:             true,
		DoNotReference:             true,
		Anonymous:                  true,
		AllowAdditionalProperties:  false,
		RequiredFromJSONSchemaTags: false,
	}
	b, err := json.Marshal(reflector.Reflect(v))
	if err != nil {
		return map[string]any{"type": "object"}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"type": "object"}
	}
	for _, k := range []string{"$schema", "$id", "$defs", "definitions"} {
		delete(m, k)
	}
	return m
}

// decodeArgs converts a tool-call argument map into a typed params struct.
func decodeArgs(args map[string]any, v any) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
