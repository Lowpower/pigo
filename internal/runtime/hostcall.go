package runtime

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/slash"
)

func (e *Engine) setBusy(v bool) {
	e.mu.Lock()
	e.busy = v
	e.mu.Unlock()
}

// IsIdle reports whether no agent turn is in flight.
func (e *Engine) IsIdle() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.busy && !e.compacting
}

// SetKick is called by TUI/RPC to start a turn from an extension.
func (e *Engine) SetKick(fn func(user string, images []ai.ImageContent)) {
	e.mu.Lock()
	e.kickFn = fn
	e.mu.Unlock()
}

// SetRunAbort stores the cancel func for the in-flight turn.
func (e *Engine) SetRunAbort(fn context.CancelFunc) {
	e.mu.Lock()
	e.runAbort = fn
	e.mu.Unlock()
}

func (e *Engine) requestShutdown() {
	e.mu.Lock()
	e.wantShutdown = true
	e.mu.Unlock()
}

// WantShutdown reports whether an extension asked the host to exit.
func (e *Engine) WantShutdown() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.wantShutdown
}

func (e *Engine) modeName() string {
	switch e.Opts.InputSource {
	case "tui":
		return "tui"
	case "rpc":
		return "rpc"
	case "json":
		return "json"
	default:
		return "print"
	}
}

func (e *Engine) hasUI() bool {
	m := e.modeName()
	return m == "tui" || m == "rpc"
}

func asStringMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	s, _ := args[key].(string)
	return s
}

func argBool(args map[string]any, key string, def bool) bool {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	}
	return def
}

func argStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// HandleHostCall is the session/runtime side of host_request.
func (e *Engine) HandleHostCall(h *ext.Host, name string, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	switch name {
	case "events.on":
		h.SubscribeBus(argString(args, "event"))
		return map[string]any{"ok": true}
	case "events.emit":
		e.fanoutBus(argString(args, "event"), asStringMap(args["data"]))
		return map[string]any{"ok": true}
	case "getContextUsage":
		return e.contextUsageMap()
	case "isIdle":
		return map[string]any{"idle": e.IsIdle()}
	case "hasPendingMessages":
		return map[string]any{"pending": e.pendingCount() > 0}
	case "abort":
		e.mu.Lock()
		fn := e.runAbort
		e.mu.Unlock()
		if fn != nil {
			fn()
		}
		e.AbortRetry()
		e.AbortCompact()
		return map[string]any{"ok": true}
	case "shutdown":
		e.requestShutdown()
		return map[string]any{"ok": true}
	case "waitForIdle":
		deadline := time.Now().Add(120 * time.Second)
		for time.Now().Before(deadline) {
			if e.IsIdle() {
				return map[string]any{"idle": true}
			}
			time.Sleep(50 * time.Millisecond)
		}
		return map[string]any{"idle": e.IsIdle()}
	case "mode":
		return map[string]any{"mode": e.modeName(), "hasUI": e.hasUI(), "cwd": e.Opts.Cwd, "trusted": e.Opts.ProjectTrusted}
	case "getSystemPrompt":
		return map[string]any{"prompt": e.System}
	case "getSystemPromptOptions":
		return map[string]any{
			"cwd": e.Opts.Cwd, "agentDir": e.Opts.AgentDir, "custom": e.Opts.SystemPrompt,
			"append": e.Opts.AppendSystem, "noContextFiles": e.Opts.NoContextFiles, "projectTrusted": e.Opts.ProjectTrusted,
		}
	case "compact":
		go func() {
			_, _, _ = e.CompactNow(context.Background(), e.History(), argString(args, "customInstructions"))
		}()
		return map[string]any{"ok": true}
	case "reload":
		e.Reload()
		return map[string]any{"ok": true}
	case "getActiveTools":
		return map[string]any{"names": e.activeToolNamesCopy()}
	case "getAllTools":
		return map[string]any{"tools": e.allToolInfo()}
	case "setActiveTools":
		added := e.SetActiveTools(argStringSlice(args["names"]))
		if added == nil {
			added = []string{}
		}
		return map[string]any{"added": added, "active": e.activeToolNamesCopy()}
	case "getCommands":
		return map[string]any{"commands": e.commandInfo()}
	case "setModel":
		ok := e.applyExtModel(argString(args, "provider"), argString(args, "id"))
		return map[string]any{"ok": ok}
	case "getThinkingLevel":
		return map[string]any{"level": e.Opts.Config.Thinking}
	case "setThinkingLevel":
		lvl := argString(args, "level")
		if models.IsThinkingLevel(lvl) {
			e.Opts.Config.Thinking = lvl
			if e.Opts.Session != nil {
				_, _ = e.Opts.Session.AppendThinkingLevelChange(lvl)
			}
			e.DispatchEvent(context.Background(), "thinking_level_select", map[string]any{"thinkingLevel": lvl})
		}
		return map[string]any{"level": e.Opts.Config.Thinking}
	case "session.info":
		return e.sessionInfo()
	case "session.entries":
		return map[string]any{"entries": e.sessionEntries()}
	case "session.branch":
		return map[string]any{"entries": e.sessionBranch()}
	case "session.leaf":
		id := ""
		if e.Opts.Session != nil {
			id = e.Opts.Session.LeafID()
		}
		return map[string]any{"id": id}
	case "appendEntry":
		return e.doAppendEntry(argString(args, "customType"), args["data"])
	case "setLabel":
		return e.doSetLabel(argString(args, "entryId"), argString(args, "label"))
	case "setSessionName":
		if e.Opts.Session != nil {
			e.Opts.Session.SetName(argString(args, "name"))
			_, _ = e.Opts.Session.AppendSessionInfo(argString(args, "name"))
		}
		return map[string]any{"ok": true}
	case "getSessionName":
		name := ""
		if e.Opts.Session != nil {
			name = e.Opts.Session.Name()
		}
		return map[string]any{"name": name}
	case "sendMessage":
		return e.doSendMessage(args)
	case "sendUserMessage":
		return e.doSendUserMessage(h, args)
	case "newSession":
		ok := e.NewSession(argString(args, "parentSession"))
		return map[string]any{"cancelled": !ok}
	case "fork":
		return e.doFork(argString(args, "entryId"), argString(args, "position"))
	case "navigateTree":
		opts := session.NavigateOpts{
			Summarize:           argBool(args, "summarize", false),
			CustomInstructions:  argString(args, "customInstructions"),
			ReplaceInstructions: argBool(args, "replaceInstructions", false),
			Label:               argString(args, "label"),
		}
		res, err := e.NavigateTree(context.Background(), argString(args, "targetId"), opts)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		return map[string]any{"cancelled": false, "oldLeaf": res.OldLeafID, "newLeaf": res.NewLeafID}
	case "switchSession":
		m, err := e.SwitchSession(argString(args, "path"))
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		if m == nil {
			return map[string]any{"cancelled": true}
		}
		return map[string]any{"cancelled": false, "file": m.File(), "id": m.ID()}
	case "exec":
		return e.doExec(args)
	case "model.list", "model.getAll":
		return map[string]any{"models": modelsToAny(models.Catalog())}
	case "model.getAvailable":
		ids := auth.AuthenticatedIDs(auth.Open(e.Opts.AgentDir))
		return map[string]any{"models": modelsToAny(models.Available(ids))}
	case "model.find":
		m, ok := models.Lookup(argString(args, "provider"), argString(args, "id"))
		if !ok {
			return map[string]any{}
		}
		return map[string]any{"model": modelToAny(m)}
	case "model.hasConfiguredAuth":
		p := argString(args, "provider")
		ids := auth.AuthenticatedIDs(auth.Open(e.Opts.AgentDir))
		ok := false
		for _, id := range ids {
			if id == p {
				ok = true
				break
			}
		}
		return map[string]any{"ok": ok}
	case "model.stream", "model.streamSimple", "model.complete":
		return e.doHostModelStream(h, name, args)
	case "model.refresh":
		_ = models.PrepareCatalog(e.Opts.AgentDir, e.Opts.CatalogBaseURL, e.Opts.Offline)
		return map[string]any{"ok": true}
	case "registerMessageRenderer":
		e.setRenderer(&e.msgRenderers, argString(args, "customType"), h)
		return map[string]any{"ok": true}
	case "registerEntryRenderer":
		e.setRenderer(&e.entryRender, argString(args, "customType"), h)
		return map[string]any{"ok": true}
	case "registerMarkdownTransformer":
		e.mu.Lock()
		e.mdTransforms = append(e.mdTransforms, h)
		e.mu.Unlock()
		return map[string]any{"ok": true}
	case "addAutocompleteProvider":
		e.mu.Lock()
		e.autoComplete = append(e.autoComplete, autoCompleteReg{host: h, triggers: argStringSlice(args["triggerCharacters"])})
		e.mu.Unlock()
		return map[string]any{"ok": true}
	case "registerToolRenderer":
		e.setRenderer(&e.toolRender, argString(args, "name"), h)
		return map[string]any{"ok": true}
	case "model.getApiKeyAndHeaders", "model.getProviderAuth":
		return e.modelAuthPayload(argString(args, "provider"))
	case "model.getApiKeyForProvider":
		return map[string]any{"key": auth.APIKey(e.Opts.AgentDir, argString(args, "provider"))}
	case "model.isUsingOAuth":
		c, ok := auth.Get(e.Opts.AgentDir, argString(args, "provider"))
		return map[string]any{"ok": ok && c.Type == auth.TypeOAuth}
	case "model.getProvider":
		return e.providerSnapshot(argString(args, "provider"))
	case "model.getProviderDisplayName":
		return map[string]any{"name": providerDisplayName(argString(args, "provider"))}
	case "model.getProviderAuthStatus":
		return e.providerAuthStatus(argString(args, "provider"))
	default:
		return map[string]any{"error": "unknown host method: " + name}
	}
}

func (e *Engine) activeToolNamesCopy() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeToolNames == nil {
		if e.Tools == nil {
			return []string{}
		}
		var names []string
		for _, t := range e.Tools.List() {
			names = append(names, t.Name())
		}
		return names
	}
	return append([]string(nil), e.activeToolNames...)
}

func (e *Engine) allToolInfo() []map[string]any {
	if e.Tools == nil {
		return []map[string]any{}
	}
	var out []map[string]any
	for _, t := range e.Tools.List() {
		out = append(out, map[string]any{"name": t.Name(), "description": t.Description(), "parameters": t.Schema()})
	}
	return out
}

func (e *Engine) commandInfo() []map[string]any {
	var out []map[string]any
	for _, c := range e.SlashCommands() {
		out = append(out, map[string]any{"name": c.Name, "description": c.Description, "source": "extension"})
	}
	for _, c := range slash.Builtins() {
		out = append(out, map[string]any{"name": c.Name, "description": c.Description, "source": "builtin"})
	}
	for _, s := range e.Skills {
		out = append(out, map[string]any{"name": s.Name, "description": s.Description, "source": "skill"})
	}
	for _, t := range e.Templates {
		out = append(out, map[string]any{"name": t.Name, "description": t.Description, "source": "prompt"})
	}
	return out
}

func (e *Engine) sessionInfo() map[string]any {
	m := map[string]any{"cwd": e.Opts.Cwd, "mode": e.modeName(), "hasUI": e.hasUI()}
	if e.Opts.Session != nil {
		m["id"] = e.Opts.Session.ID()
		m["file"] = e.Opts.Session.File()
		m["name"] = e.Opts.Session.Name()
		m["leafId"] = e.Opts.Session.LeafID()
	}
	m["provider"] = e.Provider
	m["model"] = e.Opts.Config.ResolvedModel()
	m["thinkingLevel"] = e.Opts.Config.Thinking
	m["idle"] = e.IsIdle()
	if len(e.Scoped) > 0 {
		var scoped []map[string]any
		for _, s := range e.Scoped {
			scoped = append(scoped, map[string]any{"provider": s.Provider, "id": s.ID, "thinking": s.Thinking})
		}
		m["scopedModels"] = scoped
	}
	return m
}

func (e *Engine) sessionEntries() []map[string]any {
	if e.Opts.Session == nil {
		return []map[string]any{}
	}
	return entriesToAny(e.Opts.Session.Entries())
}

func (e *Engine) sessionBranch() []map[string]any {
	if e.Opts.Session == nil {
		return []map[string]any{}
	}
	return entriesToAny(e.Opts.Session.GetBranch(""))
}

func entriesToAny(in []session.Entry) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, e := range in {
		item := map[string]any{"id": e.ID, "type": e.Type, "timestamp": e.Timestamp}
		if e.CustomType != "" {
			item["customType"] = e.CustomType
		}
		if e.Label != nil {
			item["label"] = *e.Label
		}
		out = append(out, item)
	}
	return out
}

func (e *Engine) doAppendEntry(customType string, data any) map[string]any {
	if e.Opts.Session == nil {
		return map[string]any{"error": "no session"}
	}
	ent, err := e.Opts.Session.AppendCustomEntry(customType, data)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"id": ent.ID}
}

func (e *Engine) doSetLabel(entryID, label string) map[string]any {
	if e.Opts.Session == nil {
		return map[string]any{"error": "no session"}
	}
	ent, err := e.Opts.Session.AppendLabel(entryID, label)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"id": ent.ID}
}

func (e *Engine) doSendMessage(args map[string]any) map[string]any {
	customType := argString(args, "customType")
	content := argString(args, "content")
	display := argBool(args, "display", true)
	deliverAs := argString(args, "deliverAs")
	if deliverAs == "" {
		deliverAs = "steer"
	}
	trigger := argBool(args, "triggerTurn", false)
	if e.Opts.Session != nil && customType != "" {
		_, _ = e.Opts.Session.AppendCustomMessage(customType, content, display)
	}
	busy := !e.IsIdle()
	switch deliverAs {
	case "nextTurn":
		e.mu.Lock()
		e.nextTurn = append(e.nextTurn, ai.Message{Role: ai.RoleUser, Content: content})
		e.mu.Unlock()
	case "followUp":
		if busy {
			e.PushFollow(content)
		}
	default: // steer
		if busy {
			e.PushSteer(content)
		}
	}
	if trigger && !busy && deliverAs != "nextTurn" {
		e.kick("", nil)
	}
	return map[string]any{"ok": true}
}

func (e *Engine) doSendUserMessage(_ *ext.Host, args map[string]any) map[string]any {
	content := argString(args, "content")
	deliverAs := argString(args, "deliverAs")
	expand := argBool(args, "expandPromptTemplates", false)
	var images []ai.ImageContent
	if raw, ok := args["images"].([]any); ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &images)
	}
	busy := !e.IsIdle()
	if busy && deliverAs == "" {
		return map[string]any{"error": "deliverAs required while streaming"}
	}
	text := content
	if expand {
		prep, err := e.preparePrompt(context.Background(), content, images)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		if prep.Handled {
			return map[string]any{"ok": true, "handled": true}
		}
		text, images = prep.User, prep.Images
	}
	if busy {
		if deliverAs == "followUp" {
			e.PushFollowImages(text, images)
		} else {
			e.PushSteerImages(text, images)
		}
		return map[string]any{"ok": true}
	}
	e.kick(text, images)
	return map[string]any{"ok": true}
}

func (e *Engine) kick(user string, images []ai.ImageContent) {
	e.mu.Lock()
	fn := e.kickFn
	e.mu.Unlock()
	if fn != nil {
		fn(user, images)
	}
}

// TakeNextTurn drains messages queued with deliverAs nextTurn.
func (e *Engine) TakeNextTurn() []ai.Message {
	e.mu.Lock()
	out := e.nextTurn
	e.nextTurn = nil
	e.mu.Unlock()
	return out
}

func (e *Engine) doFork(entryID, position string) map[string]any {
	if e.Opts.Session == nil {
		return map[string]any{"error": "no session"}
	}
	if !e.sessionHook("session_before_fork") {
		return map[string]any{"cancelled": true}
	}
	if position == "" {
		position = "before"
	}
	child, text, err := e.Opts.Session.ForkFrom(entryID, e.Opts.Cwd, e.Opts.AgentDir, position)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	e.DispatchEvent(context.Background(), "session_shutdown", map[string]any{"sessionId": e.sessionID()})
	e.AdoptSession(child)
	e.finishSessionChange()
	return map[string]any{"cancelled": false, "text": text, "file": child.File(), "id": child.ID()}
}

func (e *Engine) doExec(args map[string]any) map[string]any {
	cmdName := argString(args, "command")
	if cmdName == "" {
		return map[string]any{"error": "command required"}
	}
	argv := argStringSlice(args["args"])
	timeout := 30 * time.Second
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		timeout = time.Duration(v) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdName, argv...)
	cmd.Dir = e.Opts.Cwd
	out, err := cmd.CombinedOutput()
	code := 0
	killed := false
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
		if ctx.Err() != nil {
			killed = true
		}
	}
	return map[string]any{"stdout": string(out), "stderr": "", "code": code, "killed": killed}
}

func (e *Engine) applyExtModel(provider, id string) bool {
	if provider == "" || id == "" {
		return false
	}
	store := auth.Open(e.Opts.AgentDir)
	ok := false
	for _, p := range auth.AuthenticatedIDs(store) {
		if p == provider {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	e.Provider = provider
	e.Opts.Config.Provider = provider
	e.Opts.Config.Model = id
	if fn := e.bindStream(provider); fn != nil {
		e.Stream = fn
	}
	if e.Opts.Session != nil {
		_, _ = e.Opts.Session.AppendModelChange(provider, id)
	}
	e.DispatchEvent(context.Background(), "model_select", map[string]any{"provider": provider, "model": id, "source": "set"})
	return true
}

func (e *Engine) doHostModelStream(h *ext.Host, name string, args map[string]any) map[string]any {
	if e.Stream == nil {
		return map[string]any{"error": "no stream"}
	}
	var msgs []ai.Message
	if raw, ok := args["messages"]; ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &msgs)
	}
	req := ai.Context{System: argString(args, "system"), Messages: msgs}
	sf := e.Stream
	es, err := sf(context.Background(), req, ai.Options{
		Model:    e.Opts.Config.ResolvedModel(),
		Thinking: e.Opts.Config.Thinking,
	})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if name == "model.complete" {
		_, final := es.Collect()
		if final == nil {
			return map[string]any{"error": "no message"}
		}
		b, _ := json.Marshal(final)
		var payload map[string]any
		_ = json.Unmarshal(b, &payload)
		return map[string]any{"message": payload}
	}
	go func() {
		for ev := range es.Events() {
			b, _ := json.Marshal(ev)
			var payload map[string]any
			_ = json.Unmarshal(b, &payload)
			h.SendHostEvent(context.Background(), "model.stream", payload, false)
		}
	}()
	return map[string]any{"ok": true}
}

func (e *Engine) contextUsageMap() map[string]any {
	window := e.contextWindow()
	var tokens int
	for _, m := range e.History() {
		if m.Assistant != nil {
			tokens += m.Assistant.Usage.Input + m.Assistant.Usage.Output
		} else {
			tokens += len(m.Content) / 4
		}
	}
	var pct any
	if window > 0 && tokens > 0 {
		pct = float64(tokens) * 100 / float64(window)
	}
	var tok any = tokens
	if tokens == 0 {
		tok = nil
		pct = nil
	}
	return map[string]any{"tokens": tok, "contextWindow": window, "percent": pct}
}

func (e *Engine) fanoutBus(event string, data map[string]any) {
	if event == "" {
		return
	}
	for _, h := range e.Hosts {
		if h != nil && h.WantsBus(event) {
			h.SendHostEvent(context.Background(), "bus", map[string]any{"event": event, "data": data}, false)
		}
	}
}

func (e *Engine) setRenderer(dst *map[string]*ext.Host, customType string, h *ext.Host) {
	if customType == "" || h == nil {
		return
	}
	e.mu.Lock()
	if *dst == nil {
		*dst = map[string]*ext.Host{}
	}
	(*dst)[customType] = h
	e.mu.Unlock()
}

// TransformMarkdown runs registered extension markdown transformers.
func (e *Engine) TransformMarkdown(markdown, messageType string, streaming bool) string {
	e.mu.Lock()
	hosts := append([]*ext.Host(nil), e.mdTransforms...)
	e.mu.Unlock()
	out := markdown
	for _, h := range hosts {
		if h == nil {
			continue
		}
		res := h.SendHostEvent(context.Background(), "markdown.transform", map[string]any{
			"markdown": out, "messageType": messageType, "isStreaming": streaming,
		}, true)
		if s := argString(res, "markdown"); s != "" {
			out = s
		}
	}
	return out
}

// RenderCustom asks the extension that registered a renderer for text/markdown.
func (e *Engine) RenderCustom(kind, customType string, data any) string {
	e.mu.Lock()
	var h *ext.Host
	if kind == "entry" {
		h = e.entryRender[customType]
	} else {
		h = e.msgRenderers[customType]
	}
	e.mu.Unlock()
	if h == nil {
		return ""
	}
	res := h.SendHostEvent(context.Background(), "render."+kind, map[string]any{"customType": customType, "data": data}, true)
	if s := argString(res, "text"); s != "" {
		return s
	}
	return argString(res, "markdown")
}

// AutocompleteQuery asks registered extension providers for suggestions.
func (e *Engine) AutocompleteQuery(text string) []map[string]any {
	e.mu.Lock()
	regs := append([]autoCompleteReg(nil), e.autoComplete...)
	e.mu.Unlock()
	var out []map[string]any
	for _, r := range regs {
		if r.host == nil {
			continue
		}
		if len(r.triggers) > 0 && !hasTrigger(text, r.triggers) {
			continue
		}
		res := r.host.SendHostEvent(context.Background(), "autocomplete.query", map[string]any{
			"text": text, "triggerCharacters": r.triggers,
		}, true)
		out = append(out, anyMaps(res["items"])...)
	}
	return out
}

// CommandArgCompletions asks the extension that owns a slash command for items.
func (e *Engine) CommandArgCompletions(name, prefix string) []map[string]any {
	e.mu.Lock()
	var host *ext.Host
	orig := name
	for _, c := range e.extCommands {
		if c.Name == name {
			host = c.Host
			orig = c.Orig
			break
		}
	}
	e.mu.Unlock()
	if host == nil {
		return nil
	}
	res := host.SendHostEvent(context.Background(), "command.complete", map[string]any{
		"name": orig, "prefix": prefix,
	}, true)
	return anyMaps(res["items"])
}

func hasTrigger(text string, triggers []string) bool {
	if text == "" {
		return false
	}
	for _, t := range triggers {
		if t != "" && strings.Contains(text, t) {
			return true
		}
	}
	return false
}

func anyMaps(v any) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func (e *Engine) modelAuthPayload(provider string) map[string]any {
	if provider == "" {
		provider = e.Provider
	}
	p, ok := auth.Lookup(provider)
	if !ok {
		return map[string]any{"error": "unknown provider: " + provider}
	}
	res, err := auth.Resolve(context.Background(), auth.Open(e.Opts.AgentDir), p, auth.ResolveOpts{})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if res == nil {
		return map[string]any{}
	}
	headers := res.Auth.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	return map[string]any{
		"apiKey":  res.Auth.APIKey,
		"headers": headers,
		"baseURL": res.Auth.BaseURL,
		"source":  res.Source,
	}
}

func (e *Engine) providerSnapshot(id string) map[string]any {
	if id == "" {
		id = e.Provider
	}
	spec, ok := models.LookupProvider(id)
	if !ok {
		return map[string]any{}
	}
	return map[string]any{
		"id": spec.ID, "name": spec.Name, "baseURL": spec.BaseURL,
		"defaultAPI": spec.DefaultAPI, "defaultID": spec.DefaultID, "env": spec.Env,
	}
}

func providerDisplayName(id string) string {
	if p, ok := auth.Lookup(id); ok {
		if p.APIKey != nil && p.APIKey.Name != "" {
			return p.APIKey.Name
		}
		if p.OAuth != nil {
			return p.OAuth.Name()
		}
	}
	if spec, ok := models.LookupProvider(id); ok && spec.Name != "" {
		return spec.Name
	}
	return id
}

func (e *Engine) providerAuthStatus(id string) map[string]any {
	if id == "" {
		id = e.Provider
	}
	st := auth.CheckAuth(auth.Open(e.Opts.AgentDir), id)
	if st == nil {
		return map[string]any{"ok": false}
	}
	return map[string]any{"ok": true, "type": st.Type, "source": st.Source}
}

// RenderTool asks a registered tool renderer for call/result text.
func (e *Engine) RenderTool(phase, name string, args any, result string) string {
	e.mu.Lock()
	h := e.toolRender[name]
	e.mu.Unlock()
	if h == nil {
		return ""
	}
	res := h.SendHostEvent(context.Background(), "render.tool", map[string]any{
		"phase": phase, "name": name, "args": args, "result": result,
	}, true)
	if s := argString(res, "text"); s != "" {
		return s
	}
	return argString(res, "markdown")
}

func modelsToAny(in []models.Model) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, m := range in {
		out = append(out, modelToAny(m))
	}
	return out
}

func modelToAny(m models.Model) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
