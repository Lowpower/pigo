package runtime

import (
	"context"
	"strings"

	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/toolsearch"
)

// searchTool is the built-in tool_search. It stays registered and out of the
// session until defaultTools or --tools names it.
type searchTool struct {
	engine *Engine
}

func (searchTool) Name() string { return toolsearch.ToolName }

func (searchTool) Description() string { return toolsearch.Description }

func (searchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query for deferred tools.",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Maximum number of tools to return. Defaults to 8.",
			},
		},
		"required": []any{"query"},
	}
}

func (t searchTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	if t.engine == nil {
		return "tool_search is not available", true
	}
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return toolsearch.ErrQuery.Error(), true
	}
	limit, err := toolsearch.ParseLimit(args["limit"])
	if err != nil {
		return err.Error(), true
	}
	if err := t.engine.waitSearchServers(ctx); err != nil {
		return err.Error(), true
	}
	t.engine.installMCPTools()
	found := t.engine.rankSearch(query, limit)
	if len(found) == 0 {
		return toolsearch.FormatResult(nil, nil), false
	}
	names := make([]string, len(found))
	desc := map[string]string{}
	for i, hit := range found {
		names[i] = hit.name
		desc[hit.name] = hit.description
	}
	active := t.engine.activeSnapshot()
	t.engine.SetActiveTools(append(active, names...))
	return toolsearch.FormatResult(names, desc), false
}

type searchHit struct {
	name        string
	description string
}

func (e *Engine) waitSearchServers(ctx context.Context) error {
	if e == nil || e.mcp == nil {
		return nil
	}
	return e.mcp.WaitEnabled(ctx)
}

func (e *Engine) rankSearch(query string, limit int) []searchHit {
	if e == nil || e.Tools == nil {
		return nil
	}
	active := map[string]bool{}
	for _, name := range e.activeSnapshot() {
		active[name] = true
	}
	var metas []toolsearch.Meta
	desc := map[string]string{}
	for _, tool := range e.Tools.List() {
		name := tool.Name()
		if name == "" || active[name] {
			continue
		}
		switch e.exposureOf(name) {
		case ext.ExposureCodeMode, ext.ExposureDeferred:
		default:
			continue
		}
		meta := toolsearch.Meta{Name: name, Description: tool.Description(), Schema: tool.Schema()}
		if info, ok := e.toolInfo(name); ok {
			if info.Description != "" {
				meta.Description = info.Description
			}
			if info.Schema != nil {
				meta.Schema = info.Schema
			}
			meta.NamespaceName = info.Namespace
		}
		if e.mcp != nil {
			if ns, ok := e.mcp.NamespaceText(name); ok {
				meta.NamespaceName = ns.Name
				meta.NamespaceDescription = ns.Description
				meta.NamespaceInstructions = ns.Instructions
			}
		}
		metas = append(metas, meta)
		desc[name] = meta.Description
	}
	docs := make([]toolsearch.Doc, len(metas))
	for i, meta := range metas {
		docs[i] = toolsearch.Document(meta)
	}
	ranked := toolsearch.Rank(query, docs, limit)
	out := make([]searchHit, 0, len(ranked))
	for _, hit := range ranked {
		out = append(out, searchHit{name: hit.Name, description: desc[hit.Name]})
	}
	return out
}
