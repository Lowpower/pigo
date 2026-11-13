package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/mcp"
	"github.com/Lowpower/pigo/internal/mcp/oauth"
)

func (e *Engine) startMCP(ctx context.Context) {
	if e == nil || e.Opts.NoTools || e.Opts.NoMCP {
		return
	}
	var regs []mcp.Server
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		for _, reg := range h.MCPServers() {
			srv, err := mcp.ServerFromMap(reg.Name, reg.Args, mcp.SourceExtension)
			if err != nil {
				e.addExtWarning("Warning: mcp server " + strconv.Quote(reg.Name) + ": " + err.Error())
				continue
			}
			regs = append(regs, srv)
		}
	}
	fileServers, diags := mcp.Load(e.Opts.AgentDir, e.Opts.Cwd, e.Opts.ProjectTrusted)
	for _, d := range diags {
		e.addExtWarning("Warning: " + d.Error())
	}
	servers, more := mcp.MergeExtensions(fileServers, regs)
	for _, d := range more {
		e.addExtWarning("Warning: " + d.Error())
	}
	mgr := mcp.New(servers, e.Opts.Cwd)
	e.mcp = mgr
	mgr.SetOnChange(func() { e.onMCPChange() })
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		h.SetMCPHook(func(name string, args map[string]any) {
			e.addExtensionMCP(name, args)
		})
	}
	mgr.Start(ctx)
}

func (e *Engine) addExtensionMCP(name string, args map[string]any) {
	if e == nil || e.mcp == nil {
		return
	}
	srv, err := mcp.ServerFromMap(name, args, mcp.SourceExtension)
	if err != nil {
		e.addExtWarning("Warning: mcp server " + strconv.Quote(name) + ": " + err.Error())
		return
	}
	for _, st := range e.mcp.Statuses() {
		if st.Name == name {
			return
		}
		if mcp.Canon(st.Name) == mcp.Canon(name) {
			e.addExtWarning("Warning: servers " + strconv.Quote(st.Name) + " and " + strconv.Quote(name) + " differ only by '-' and '_'")
			return
		}
	}
	e.mcp.Add(srv)
}

// MCPStatuses is the current server list. It is empty when MCP is not loaded.
func (e *Engine) MCPStatuses() []mcp.Status {
	if e == nil || e.mcp == nil {
		return nil
	}
	return e.mcp.Statuses()
}

// MCPServer returns one configured server, including extension registrations.
func (e *Engine) MCPServer(name string) (mcp.Server, bool) {
	if e == nil || e.mcp == nil {
		return mcp.Server{}, false
	}
	return e.mcp.Server(name)
}

// SetMCPEnabled turns one server on or off. File-backed servers are written
// back to the mcp.json that defines them. Extension servers change this session.
func (e *Engine) SetMCPEnabled(name string, enabled bool) error {
	if e == nil || e.mcp == nil {
		return errors.New("mcp is not loaded")
	}
	srv, ok := e.mcp.Server(name)
	if !ok {
		return fmt.Errorf("unknown mcp server %q", name)
	}
	if srv.Source != mcp.SourceExtension && srv.File != "" {
		if err := mcp.SetEnabled(srv.File, name, enabled); err != nil {
			return err
		}
	}
	return e.mcp.SetEnabled(name, enabled)
}

// ReconnectMCP connects one server again and leaves the others running.
func (e *Engine) ReconnectMCP(name string) error {
	if e == nil || e.mcp == nil {
		return errors.New("mcp is not loaded")
	}
	return e.mcp.Reconnect(name)
}

// MCPLogin runs OAuth for one HTTP server. ctx limits the whole sign-in.
func (e *Engine) MCPLogin(ctx context.Context, name string, prompt oauth.Prompt) error {
	if e == nil || e.mcp == nil {
		return errors.New("mcp is not loaded")
	}
	srv, ok := e.mcp.Server(name)
	if !ok {
		return fmt.Errorf("unknown mcp server %q", name)
	}
	return mcp.Login(ctx, e.Opts.AgentDir, srv, prompt)
}

func (e *Engine) closeMCP() {
	if e == nil || e.mcp == nil {
		return
	}
	e.mcp.Close()
}

func (e *Engine) waitDirectMCP(ctx context.Context) error {
	if e == nil || e.mcp == nil {
		return nil
	}
	return e.mcp.WaitDirect(ctx)
}

func (e *Engine) waitMCPForCall(ctx context.Context, name string, args map[string]any) {
	if e == nil || e.mcp == nil {
		return
	}
	_ = e.mcp.WaitFor(ctx, name, args, e.ReachesTools(name))
	e.installMCPTools()
}

func (e *Engine) mcpSection() string {
	if e == nil || e.mcp == nil {
		return ""
	}
	return e.mcp.Section()
}

func (e *Engine) onMCPChange() {
	if e == nil || e.mcp == nil {
		return
	}
	e.installMCPTools()
	section := e.mcp.Section()
	names := mcpToolNames(e.mcp.Tools())
	e.mcpMu.Lock()
	changed := section != e.mcpSectionText || !sameStringList(names, e.mcpEventNames)
	e.mcpSectionText = section
	e.mcpEventNames = names
	e.mcpMu.Unlock()
	if e.Opts.Session == nil {
		e.rebuildSystemPrompt()
	} else {
		e.syncToolsIfDeclared()
	}
	if changed {
		e.DispatchEvent(context.Background(), "mcp_servers_change", e.mcpEvent())
	}
}

func (e *Engine) mcpEvent() map[string]any {
	if e == nil || e.mcp == nil {
		return map[string]any{"servers": []any{}}
	}
	statuses := e.mcp.Statuses()
	servers := make([]any, 0, len(statuses))
	for _, st := range statuses {
		servers = append(servers, map[string]any{
			"name":         st.Name,
			"exposure":     st.Exposure,
			"description":  st.Description,
			"status":       st.Status,
			"hasResources": st.HasResources,
		})
	}
	return map[string]any{"servers": servers}
}

func (e *Engine) installMCPTools() {
	if e == nil || e.mcp == nil || e.Tools == nil {
		return
	}
	specs := e.mcp.Tools()
	infos := make(map[string]ext.ToolInfo, len(specs))
	names := make(map[string]bool, len(specs))
	for _, spec := range specs {
		names[spec.Name] = true
		infos[spec.Name] = ext.ToolInfo{
			Name:         spec.Name,
			Description:  spec.Description,
			Schema:       spec.Schema,
			Exposure:     spec.Exposure,
			Namespace:    spec.Namespace,
			Aliases:      append([]string(nil), spec.Aliases...),
			OutputSchema: spec.OutputSchema,
		}
		e.Tools.Upsert(mcpRuntimeTool{mgr: e.mcp, spec: spec})
	}
	e.mcpMu.Lock()
	prev := e.mcpNames
	drop := map[string]bool{}
	for name := range prev {
		if !names[name] {
			drop[name] = true
		}
	}
	e.mcpNames = names
	e.mcpInfos = infos
	e.mcpMu.Unlock()
	e.Tools.Drop(drop)
	e.adoptMCPTools(specs, prev)
}

// adoptMCPTools applies the --tools rule once a server has registered tools.
// A plain list keeps MCP tools, or only the mcp__ matches. Otherwise a tool
// joins the enabled set the first time it appears.
func (e *Engine) adoptMCPTools(specs []mcp.ToolSpec, prev map[string]bool) {
	if e == nil || e.Opts.NoTools || e.Tools == nil {
		return
	}
	if e.explicitToolSelection() {
		e.installActiveTools(e.selectedActiveNames())
		return
	}
	names := e.activeSnapshot()
	have := map[string]bool{}
	for _, name := range names {
		have[name] = true
	}
	changed := false
	for _, spec := range specs {
		if prev[spec.Name] || have[spec.Name] || config.ToolMatchesAny(e.Opts.ToolDeny, spec.Name) {
			continue
		}
		if !e.modelVisible(spec.Name) {
			continue
		}
		names = append(names, spec.Name)
		changed = true
	}
	if changed {
		e.installActiveTools(names)
	}
}

func (e *Engine) mcpInfo(name string) (ext.ToolInfo, bool) {
	if e == nil || name == "" {
		return ext.ToolInfo{}, false
	}
	e.mcpMu.RLock()
	defer e.mcpMu.RUnlock()
	info, ok := e.mcpInfos[name]
	return info, ok
}

func (e *Engine) mcpAlias(name string) (ext.ToolInfo, bool) {
	if e == nil || name == "" {
		return ext.ToolInfo{}, false
	}
	e.mcpMu.RLock()
	defer e.mcpMu.RUnlock()
	for _, info := range e.mcpInfos {
		for _, alias := range info.Aliases {
			if alias == name {
				return info, true
			}
		}
	}
	return ext.ToolInfo{}, false
}

func (e *Engine) mcpNamespace(ns string) []ext.ToolInfo {
	want := mcp.CanonNamespace(ns)
	if e == nil || want == "" {
		return nil
	}
	e.mcpMu.RLock()
	defer e.mcpMu.RUnlock()
	var out []ext.ToolInfo
	for _, info := range e.mcpInfos {
		if mcp.CanonNamespace(info.Namespace) == want {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (e *Engine) addExtWarning(msg string) {
	if e == nil || msg == "" {
		return
	}
	e.mu.Lock()
	e.extLoadWarnings = append(e.extLoadWarnings, msg)
	e.mu.Unlock()
}

type mcpRuntimeTool struct {
	mgr  *mcp.Manager
	spec mcp.ToolSpec
}

func (t mcpRuntimeTool) Name() string           { return t.spec.Name }
func (t mcpRuntimeTool) Description() string    { return t.spec.Description }
func (t mcpRuntimeTool) Schema() map[string]any { return t.spec.Schema }
func (t mcpRuntimeTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	res, err := t.mgr.Call(ctx, t.spec.Server, t.spec.Remote, args)
	if err != nil {
		return err.Error(), true
	}
	if box := ext.OutcomeBox(ctx); box != nil && len(res.Structured) > 0 {
		box.Structured = append(json.RawMessage(nil), res.Structured...)
	}
	return res.Text, res.IsError
}

func mcpToolNames(specs []mcp.ToolSpec) []string {
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.Name)
	}
	sort.Strings(out)
	return out
}

func sameStringList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
