package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/codescript"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
)

// codeTool runs one codemode script. It is registered with the built-ins and
// stays out of the session until defaultTools or --tools names it.
type codeTool struct {
	engine *Engine
}

func (codeTool) Name() string { return "codemode" }

func (codeTool) Description() string { return codescript.ToolDescription }

func (codeTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "JavaScript source. Top-level await and return work.",
			},
		},
		"required": []any{"code"},
	}
}

func (t codeTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	code, _ := args["code"].(string)
	if strings.TrimSpace(code) == "" {
		return "code is required", true
	}
	if t.engine == nil {
		return "codemode is not available", true
	}
	res, err := codescript.Run(ctx, code, t.engine.scriptEnv())
	out := res.Render()
	if err != nil {
		if out == "" {
			out = err.Error()
		} else if !strings.Contains(out, err.Error()) {
			out += "\n" + err.Error()
		}
		return out, true
	}
	return out, false
}

func (e *Engine) scriptEnv() codescript.Env {
	e.mu.Lock()
	activeSet := e.activeToolNames != nil
	active := append([]string(nil), e.activeToolNames...)
	reg := e.Tools
	if e.scriptStore == nil {
		e.scriptStore = &codescript.Store{}
	}
	store := e.scriptStore
	e.mu.Unlock()

	var list []codescript.Tool
	if reg != nil {
		for _, tool := range reg.List() {
			item := codescript.Tool{
				Name:        tool.Name(),
				Description: tool.Description(),
				Schema:      tool.Schema(),
				Exposure:    ext.ExposureDirect,
			}
			if info, ok := e.toolInfo(tool.Name()); ok {
				item.Exposure = info.Exposure
				item.Namespace = info.Namespace
				item.Aliases = info.Aliases
				item.OutputSchema = info.OutputSchema
				if item.Description == "" {
					item.Description = info.Description
				}
				if item.Schema == nil {
					item.Schema = info.Schema
				}
			}
			list = append(list, item)
		}
	}
	return codescript.Env{
		Tools:     list,
		Active:    active,
		ActiveSet: activeSet,
		Store:     store,
		Exec: func(ctx context.Context, name string, args map[string]any) (string, bool) {
			if name == "codemode" || e.Tools == nil {
				return "unknown tool: " + name, true
			}
			return e.Tools.Execute(ctx, name, args)
		},
		Models: e.scriptModels,
	}
}

func (e *Engine) scriptModels(typ, provider string) ([]codescript.ModelInfo, error) {
	switch typ {
	case "chat", "classifier", "image":
	default:
		return nil, fmt.Errorf("unknown model type %q", typ)
	}
	if typ != "chat" {
		return []codescript.ModelInfo{}, nil
	}
	var out []codescript.ModelInfo
	for _, m := range models.Available(auth.AuthenticatedIDs(auth.Open(e.Opts.AgentDir))) {
		if provider != "" && m.Provider != provider {
			continue
		}
		out = append(out, codescript.ModelInfo{
			Type:          "chat",
			Provider:      m.Provider,
			ID:            m.ID,
			Name:          m.Name,
			API:           m.API,
			Input:         append([]string(nil), m.Input...),
			ContextWindow: m.ContextWindow,
		})
	}
	if out == nil {
		out = []codescript.ModelInfo{}
	}
	return out, nil
}
