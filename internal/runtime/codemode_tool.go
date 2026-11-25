package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/codescript"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/tools"
)

// codeTool runs one codemode script. It is registered with the built-ins and
// stays out of the session until defaultTools or --tools names it.
type codeTool struct {
	engine *Engine
}

func (codeTool) Name() string { return "codemode" }

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

func (t codeTool) Description() string {
	base := codescript.ToolDescription
	if t.engine == nil {
		return base
	}
	extra := t.engine.codemodeListing()
	if extra == "" {
		return base
	}
	return base + "\n\n" + extra
}

func (t codeTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	code, _ := args["code"].(string)
	if strings.TrimSpace(code) == "" {
		return "code is required", true
	}
	if t.engine == nil {
		return "codemode is not available", true
	}
	started := time.Now()
	res, err := codescript.Run(ctx, code, t.engine.scriptEnv())
	if err != nil {
		return res.WithErrorTook(time.Since(started), err), true
	}
	return res.WithTook(time.Since(started)), false
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
			// The model-facing codemode description lists other tools, so reading
			// it here would call scriptEnv again.
			desc := codescript.ToolDescription
			if tool.Name() != "codemode" {
				desc = tool.Description()
			}
			item := codescript.Tool{
				Name:        tool.Name(),
				Description: desc,
				Schema:      tool.Schema(),
				Exposure:    e.exposureOf(tool.Name()),
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
			if item.OutputSchema == nil {
				if s, ok := tool.(tools.OutputSchemer); ok {
					item.OutputSchema = s.OutputSchema()
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
			if name == "bash" || name == "powershell" {
				box := &ext.ToolResult{}
				ctx = ext.WithOutcome(ctx, box)
				text, isErr := e.Tools.Execute(ctx, name, args)
				if len(box.Structured) > 0 {
					return string(box.Structured), isErr
				}
				return text, isErr
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
	var out []codescript.ModelInfo
	for _, m := range models.AvailableOfType(typ, auth.AuthenticatedIDs(auth.Open(e.Opts.AgentDir))) {
		if provider != "" && m.Provider != provider {
			continue
		}
		out = append(out, codescript.ModelInfo{
			Type:          m.ModelType(),
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

func (e *Engine) codemodeListing() string {
	if !e.codemodeActive() {
		return ""
	}
	env := e.scriptEnv()
	tools := codescript.Callable(env)
	if e.Opts.Config.CodemodeMode() != "only" {
		declared := map[string]bool{}
		for _, name := range e.modelFacingNames() {
			declared[name] = true
		}
		kept := make([]codescript.Tool, 0, len(tools))
		for _, tool := range tools {
			if !declared[tool.Name] {
				kept = append(kept, tool)
			}
		}
		tools = kept
	}
	return codescript.FormatListing(tools, e.Opts.Config.CodemodeInlineBudget())
}

func (e *Engine) codemodeActive() bool {
	if e == nil {
		return false
	}
	for _, name := range e.activeSnapshot() {
		if name == "codemode" {
			return true
		}
	}
	return false
}

func (e *Engine) codemodeOnly() bool {
	return e.codemodeActive() && e.Opts.Config.CodemodeMode() == "only"
}

// presentTools applies codemode.mode to the model-facing tool list.
// on appends a script-call note. only keeps codemode when that tool is active.
func (e *Engine) presentTools(tools []ai.Tool) []ai.Tool {
	if e == nil || len(tools) == 0 {
		return tools
	}
	if e.codemodeActive() && e.Opts.Config.CodemodeMode() == "on" {
		out := append([]ai.Tool(nil), tools...)
		for i, tool := range out {
			if tool.Name == "" || tool.Name == "codemode" {
				continue
			}
			note := "Callable from a codemode script as await tools." + codescript.JSName(tool.Name) + "(args)."
			if strings.Contains(tool.Description, note) {
				continue
			}
			if strings.TrimSpace(tool.Description) == "" {
				tool.Description = note
			} else {
				tool.Description = tool.Description + " " + note
			}
			out[i] = tool
		}
		tools = out
	}
	if e.codemodeOnly() {
		return filterAITools(tools, []string{"codemode"})
	}
	return tools
}
