package tools

import (
	"context"
	"encoding/json"

	"github.com/invopop/jsonschema"

	"github.com/Lowpower/pigo/internal/ai"
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

// Default returns a registry with all seven built-in tools.
func Default() *Registry {
	return NewRegistry(
		readTool{}, writeTool{}, editTool{}, bashTool{}, grepTool{}, findTool{}, listTool{},
	)
}

// AITools returns the tools as provider-neutral ai.Tool definitions.
func (r *Registry) AITools() []ai.Tool {
	out := make([]ai.Tool, 0, len(r.order))
	for _, t := range r.order {
		out = append(out, ai.Tool{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()})
	}
	return out
}

// List returns tools in registration order.
func (r *Registry) List() []Tool {
	out := make([]Tool, len(r.order))
	copy(out, r.order)
	return out
}

// Execute dispatches a tool call by name. It satisfies the shape expected by an
// agent ToolExecutor adapter: Execute(ctx, name, args).
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, bool) {
	t, ok := r.byName[name]
	if !ok {
		return "unknown tool: " + name, true
	}
	return t.Execute(ctx, args)
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
