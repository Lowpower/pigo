package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/compaction"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/pkgmgr"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/skills"
	"github.com/Lowpower/pigo/internal/tools"
)

// Options is the shared engine used by TUI, print, json, and rpc modes.
type Options struct {
	Config         config.Config
	Cwd            string
	AgentDir       string
	Session        *session.Manager // nil when --no-session
	SystemPrompt   string
	AppendSystem   []string
	NoContextFiles bool
	NoSkills       bool
	SkillPaths     []string
	NoTools        bool
	NoBuiltinTools bool
	ToolAllow      []string
	ToolDeny       []string
	Extensions     []string // argv[0] of each extension binary (CLI -e plus discovered)
	CLIExtensions  []string // explicit -e; kept across --no-extensions and /reload
	NoExtensions   bool     // skip auto-discovery; CLI -e still loads
	ProjectTrusted bool
	ContextWindow  int
	NoPromptTpls   bool
	PromptPaths    []string
	Models         []string // --models cycling list
}

// Engine is a configured agent runner.
type Engine struct {
	Opts      Options
	Stream    ai.StreamFn
	Provider  string
	Tools     *tools.Registry
	Hosts     []*ext.Host
	Skills    []skills.Skill
	Templates []prompt.Template
	Scoped    []models.Spec
	System    string

	Steering  func() []ai.Message
	FollowUp  func() []ai.Message
	persisted int

	mu         sync.Mutex
	steer      []ai.Message
	follow     []ai.Message
	compacting bool

	onSessionEvent func(any)
	uiHandler      uiHandlerFunc

	retryMu      sync.Mutex
	retryCancel  context.CancelFunc
	retryAttempt int
}

// New applies auth, discovers skills, loads tools/extensions, and builds the prompt.
func New(ctx context.Context, opts Options) (*Engine, error) {
	if opts.Cwd == "" {
		opts.Cwd, _ = os.Getwd()
	}
	if opts.AgentDir == "" {
		opts.AgentDir = config.DefaultConfigDir()
	}
	auth.ApplyEnv(opts.AgentDir)

	provider := opts.Config.ResolvedProvider()
	if provider == "" {
		provider = "anthropic"
	}
	sf := boundStream(opts.AgentDir, provider)
	if sf == nil {
		sf, provider = ai.DefaultStreamFn()
	}
	reg := tools.Default()
	if opts.NoTools {
		reg = tools.NewRegistry()
	} else if opts.NoBuiltinTools {
		reg = tools.NewRegistry()
	} else {
		reg = filterTools(reg, opts.ToolAllow, opts.ToolDeny)
	}

	if len(opts.CLIExtensions) == 0 && len(opts.Extensions) > 0 {
		opts.CLIExtensions = opts.Extensions
	}
	extSpecs := collectExtensionSpecs(ctx, opts)
	opts.Extensions = extSpecs

	var hosts []*ext.Host
	if !opts.NoTools {
		var err error
		hosts, reg, err = spawnExtensions(ctx, extSpecs, reg)
		if err != nil {
			return nil, err
		}
	}

	var sk []skills.Skill
	if !opts.NoSkills {
		sk, _ = skills.Discover(opts.Cwd, opts.AgentDir, opts.SkillPaths, true)
	}

	sys := prompt.Build(prompt.Options{
		Cwd:              opts.Cwd,
		Custom:           opts.SystemPrompt,
		Append:           opts.AppendSystem,
		NoContextFiles:   opts.NoContextFiles,
		Skills:           sk,
		Tools:            reg.AITools(),
		IncludeToolHints: true,
	})

	e := &Engine{
		Opts:      opts,
		Stream:    sf,
		Provider:  provider,
		Tools:     reg,
		Hosts:     hosts,
		Skills:    sk,
		Templates: prompt.DiscoverTemplates(opts.Cwd, opts.AgentDir, opts.PromptPaths, !opts.NoPromptTpls),
		Scoped:    models.ResolvePatterns(opts.Models),
		System:    sys,
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	return e, nil
}

func collectExtensionSpecs(ctx context.Context, opts Options) []string {
	var specs []string
	if !opts.NoExtensions {
		m, err := pkgmgr.Open(opts.Cwd, opts.AgentDir, opts.ProjectTrusted)
		if err == nil {
			if rs, err := m.Resolve(ctx); err == nil {
				for _, argv := range pkgmgr.SpawnArgv(rs) {
					specs = append(specs, strings.Join(argv, " "))
				}
			}
		}
	}
	specs = append(specs, opts.CLIExtensions...)
	return specs
}

func spawnExtensions(ctx context.Context, specs []string, reg *tools.Registry) ([]*ext.Host, *tools.Registry, error) {
	var hosts []*ext.Host
	for _, spec := range specs {
		argv := strings.Fields(spec)
		if len(argv) == 0 {
			continue
		}
		h, err := ext.Spawn(ctx, argv[0], argv, ext.Options{})
		if err != nil {
			return hosts, reg, fmt.Errorf("extension %q: %w", spec, err)
		}
		hosts = append(hosts, h)
		for _, t := range h.Tools() {
			reg = tools.NewRegistry(append(reg.List(), wrapExt(h, t.Name, t.Description, t.Parameters))...)
		}
	}
	return hosts, reg, nil
}

func (e *Engine) emitSession(v any) {
	if e.onSessionEvent != nil {
		e.onSessionEvent(v)
	}
}

func (e *Engine) setCompacting(v bool) {
	e.mu.Lock()
	e.compacting = v
	e.mu.Unlock()
}

func (e *Engine) isCompacting() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.compacting
}

func (e *Engine) pendingCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.steer) + len(e.follow)
}

func (e *Engine) contextWindow() int {
	window := e.Opts.ContextWindow
	if window <= 0 {
		window = e.Opts.Config.ContextWindow
	}
	if window <= 0 {
		window = 200000
	}
	return window
}

// CompactNow runs a manual compaction (RPC compact), including customInstructions.
func (e *Engine) CompactNow(ctx context.Context, msgs []ai.Message, customInstructions string) ([]ai.Message, string, error) {
	s := compaction.DefaultSettings()
	if e.Opts.Config.ReserveTokens > 0 {
		s.ReserveTokens = e.Opts.Config.ReserveTokens
	}
	if e.Opts.Config.KeepRecentTokens > 0 {
		s.KeepRecentTokens = e.Opts.Config.KeepRecentTokens
	}
	s.CustomInstructions = customInstructions
	e.setCompacting(true)
	defer e.setCompacting(false)
	e.emitSession(map[string]any{"type": "compaction_start", "reason": "manual"})
	out, summary, err := compaction.Compact(ctx, e.Stream, e.Opts.Config.ResolvedModel(), msgs, s)
	if err != nil {
		e.emitSession(map[string]any{
			"type": "compaction_end", "reason": "manual",
			"result": nil, "aborted": false, "willRetry": false, "errorMessage": err.Error(),
		})
		return msgs, "", err
	}
	if summary != "" && e.Opts.Session != nil {
		if entry, err := e.Opts.Session.AppendCompaction(summary); err == nil && entry != nil {
			e.emitSession(map[string]any{"type": "entry_appended", "entry": entry})
		}
	}
	e.emitSession(map[string]any{
		"type": "compaction_end", "reason": "manual",
		"result": map[string]any{"summary": summary}, "aborted": false, "willRetry": false,
	})
	return out, summary, nil
}

func (e *Engine) queueTexts() (steering, follow []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	steering = make([]string, 0, len(e.steer))
	for _, m := range e.steer {
		steering = append(steering, m.Content)
	}
	follow = make([]string, 0, len(e.follow))
	for _, m := range e.follow {
		follow = append(follow, m.Content)
	}
	return steering, follow
}

func (e *Engine) emitQueueUpdate() {
	s, f := e.queueTexts()
	e.emitSession(map[string]any{"type": "queue_update", "steering": s, "followUp": f})
}

func (e *Engine) queueUser(dst *[]ai.Message, text string, images []ai.ImageContent) {
	text = strings.TrimSpace(text)
	if text == "" && len(images) == 0 {
		return
	}
	e.mu.Lock()
	*dst = append(*dst, ai.Message{Role: ai.RoleUser, Content: text, Images: images})
	e.mu.Unlock()
	e.emitQueueUpdate()
}

// PushSteer queues a user message for the in-flight turn.
func (e *Engine) PushSteer(text string) {
	e.queueUser(&e.steer, text, nil)
}

// PushSteerImages queues a steering message with optional image blocks.
func (e *Engine) PushSteerImages(text string, images []ai.ImageContent) {
	e.queueUser(&e.steer, text, images)
}

// PushFollow queues a user message for after the current turn.
func (e *Engine) PushFollow(text string) {
	e.queueUser(&e.follow, text, nil)
}

// PushFollowImages queues a follow-up message with optional image blocks.
func (e *Engine) PushFollowImages(text string, images []ai.ImageContent) {
	e.queueUser(&e.follow, text, images)
}

func (e *Engine) drainSteer() []ai.Message {
	e.mu.Lock()
	out := drainQueue(&e.steer, e.Opts.Config.SteeringMode)
	e.mu.Unlock()
	if len(out) > 0 {
		e.emitQueueUpdate()
	}
	return out
}

func (e *Engine) drainFollow() []ai.Message {
	e.mu.Lock()
	out := drainQueue(&e.follow, e.Opts.Config.FollowUpMode)
	e.mu.Unlock()
	if len(out) > 0 {
		e.emitQueueUpdate()
	}
	return out
}

// drainQueue: "all" empties the queue; "one-at-a-time" (default) returns only
// the oldest message.
func drainQueue(q *[]ai.Message, mode string) []ai.Message {
	if q == nil || len(*q) == 0 {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(mode), "all") {
		out := *q
		*q = nil
		return out
	}
	out := []ai.Message{(*q)[0]}
	*q = (*q)[1:]
	return out
}

func wrapExt(h *ext.Host, name, desc string, schema map[string]any) tools.Tool {
	return extTool{host: h, name: name, desc: desc, schema: schema}
}

type extTool struct {
	host   *ext.Host
	name   string
	desc   string
	schema map[string]any
}

func (t extTool) Name() string           { return t.name }
func (t extTool) Description() string    { return t.desc }
func (t extTool) Schema() map[string]any { return t.schema }
func (t extTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	return t.host.CallTool(ctx, t.name, args)
}

func filterTools(reg *tools.Registry, allow, deny []string) *tools.Registry {
	all := reg.List()
	if len(allow) == 0 && len(deny) == 0 {
		return reg
	}
	denySet := map[string]bool{}
	for _, d := range deny {
		denySet[d] = true
	}
	allowSet := map[string]bool{}
	for _, a := range allow {
		allowSet[a] = true
	}
	var keep []tools.Tool
	for _, t := range all {
		if denySet[t.Name()] {
			continue
		}
		if len(allowSet) > 0 && !allowSet[t.Name()] {
			continue
		}
		keep = append(keep, t)
	}
	return tools.NewRegistry(keep...)
}

// Close stops extension subprocesses.
func (e *Engine) Close() {
	for _, h := range e.Hosts {
		_ = h.Close()
	}
}

// Executor returns the tool executor for the agent loop.
func (e *Engine) Executor() agent.ToolExecutor {
	return agent.ToolFunc(func(ctx context.Context, c agent.ToolCall) (string, bool) {
		if e.Tools == nil {
			return "", true
		}
		return e.Tools.Execute(ctx, c.Name, c.Args)
	})
}

// MaybeCompact runs compaction when the estimate exceeds the window.
func (e *Engine) MaybeCompact(ctx context.Context, msgs []ai.Message) ([]ai.Message, string, error) {
	if !e.Opts.Config.CompactionEnabled() {
		return msgs, "", nil
	}
	window := e.Opts.ContextWindow
	if window <= 0 {
		window = e.Opts.Config.ContextWindow
	}
	if window <= 0 {
		window = 200000
	}
	s := compaction.DefaultSettings()
	if e.Opts.Config.ReserveTokens > 0 {
		s.ReserveTokens = e.Opts.Config.ReserveTokens
	}
	if e.Opts.Config.KeepRecentTokens > 0 {
		s.KeepRecentTokens = e.Opts.Config.KeepRecentTokens
	}
	if !compaction.ShouldCompact(compaction.EstimateContextTokens(msgs), window, s) {
		return msgs, "", nil
	}
	e.setCompacting(true)
	defer e.setCompacting(false)
	e.emitSession(map[string]any{"type": "compaction_start", "reason": "threshold"})
	out, summary, err := compaction.Compact(ctx, e.Stream, e.Opts.Config.ResolvedModel(), msgs, s)
	if err != nil {
		e.emitSession(map[string]any{
			"type": "compaction_end", "reason": "threshold",
			"result": nil, "aborted": false, "willRetry": false, "errorMessage": err.Error(),
		})
		return msgs, "", err
	}
	if summary != "" && e.Opts.Session != nil {
		if entry, err := e.Opts.Session.AppendCompaction(summary); err == nil && entry != nil {
			e.emitSession(map[string]any{"type": "entry_appended", "entry": entry})
		}
	}
	e.emitSession(map[string]any{
		"type": "compaction_end", "reason": "threshold",
		"result": map[string]any{"summary": summary}, "aborted": false, "willRetry": false,
	})
	return out, summary, nil
}

// RunPrompt runs one user prompt through the agent loop (print/json/rpc/TUI).
func (e *Engine) RunPrompt(ctx context.Context, history []ai.Message, user string, images []ai.ImageContent) *agent.Stream {
	userMsg := ai.Message{Role: ai.RoleUser, Content: user, Images: images}
	history = append(append([]ai.Message(nil), history...), userMsg)
	compacted, _, err := e.MaybeCompact(ctx, history)
	if err == nil {
		history = compacted
	}
	return e.runLoop(ctx, history, []ai.Message{userMsg})
}

func (e *Engine) runLoop(ctx context.Context, history, newUsers []ai.Message) *agent.Stream {
	req := ai.Context{System: e.System, Messages: history}
	if e.Tools != nil {
		req.Tools = e.Tools.AITools()
	}
	return agent.Run(ctx, e.Stream, req, e.Executor(), agent.Config{
		Model:           e.Opts.Config.ResolvedModel(),
		Thinking:        e.Opts.Config.Thinking,
		Steering:        e.Steering,
		FollowUp:        e.FollowUp,
		NewUserMessages: newUsers,
	})
}

// AdoptSession attaches a session manager and records how many entries are already persisted.
func (e *Engine) AdoptSession(s *session.Manager) {
	e.Opts.Session = s
	if s == nil {
		e.persisted = 0
		return
	}
	e.persisted = len(s.Entries())
}

// History restores provider-facing messages from the attached session.
func (e *Engine) History() []ai.Message {
	if e.Opts.Session == nil {
		return nil
	}
	msgs := session.RestoreAIMessages(e.Opts.Session.Entries())
	e.persisted = len(msgs)
	return msgs
}

// Reload rediscovers skills, extensions, and rebuilds the system prompt (/reload).
func (e *Engine) Reload() {
	ctx := context.Background()
	if e.Opts.NoSkills {
		e.Skills = nil
	} else {
		e.Skills, _ = skills.Discover(e.Opts.Cwd, e.Opts.AgentDir, e.Opts.SkillPaths, true)
	}
	e.Templates = prompt.DiscoverTemplates(e.Opts.Cwd, e.Opts.AgentDir, e.Opts.PromptPaths, !e.Opts.NoPromptTpls)

	for _, h := range e.Hosts {
		_ = h.Close()
	}
	e.Hosts = nil
	reg := tools.Default()
	if e.Opts.NoTools {
		reg = tools.NewRegistry()
	} else if e.Opts.NoBuiltinTools {
		reg = tools.NewRegistry()
	} else {
		reg = filterTools(reg, e.Opts.ToolAllow, e.Opts.ToolDeny)
	}
	if !e.Opts.NoTools {
		specs := collectExtensionSpecs(ctx, e.Opts)
		e.Opts.Extensions = specs
		hosts, r, err := spawnExtensions(ctx, specs, reg)
		if err == nil {
			e.Hosts = hosts
			reg = r
		}
	}
	e.Tools = reg

	e.System = prompt.Build(prompt.Options{
		Cwd:              e.Opts.Cwd,
		Custom:           e.Opts.SystemPrompt,
		Append:           e.Opts.AppendSystem,
		NoContextFiles:   e.Opts.NoContextFiles,
		Skills:           e.Skills,
		Tools:            e.Tools.AITools(),
		IncludeToolHints: true,
	})
}

// PersistTranscript writes new agent messages to the session file.
func (e *Engine) PersistTranscript(msgs []agent.Msg) {
	if e.Opts.Session == nil {
		return
	}
	if e.persisted > len(msgs) {
		e.persisted = 0
	}
	for _, msg := range msgs[e.persisted:] {
		var (
			entry *session.Entry
			err   error
		)
		switch msg.Role {
		case agent.RoleAssistant:
			entry, err = e.Opts.Session.AppendMessage("assistant", msg.Assistant)
		case agent.RoleToolResult:
			entry, err = e.Opts.Session.AppendMessage("toolResult", map[string]any{
				"role": "toolResult", "toolName": msg.ToolName, "toolCallId": msg.ToolCallID,
				"content": msg.Text, "isError": msg.IsError,
			})
		default:
			payload := map[string]any{"role": "user", "content": msg.Text}
			if len(msg.Images) > 0 {
				payload["content"] = agent.UserContentBlocks(msg.Text, msg.Images)
			}
			entry, err = e.Opts.Session.AppendMessage("user", payload)
		}
		if err == nil && entry != nil {
			e.emitSession(map[string]any{"type": "entry_appended", "entry": entry})
		}
	}
	e.persisted = len(msgs)
}

// CycleModel steps through --models or the catalog (ctrl+p).
func (e *Engine) CycleModel(backward bool) (models.Spec, bool) {
	next, ok := models.Cycle(e.Opts.Config.ResolvedProvider(), e.Opts.Config.ResolvedModel(), e.Scoped, backward)
	if !ok {
		return models.Spec{}, false
	}
	e.ApplyModel(next.Provider, next.ID, next.Thinking)
	return next, true
}

// CycleThinking steps thinking levels (shift+tab).
func (e *Engine) CycleThinking() string {
	next := models.NextThinkingLevel(e.Opts.Config.Thinking)
	e.Opts.Config.Thinking = next
	return next
}

// ApplyModel sets provider/model and optional thinking override.
func (e *Engine) ApplyModel(provider, id, thinking string) {
	if provider != "" {
		e.Provider = provider
		e.Opts.Config.Provider = provider
		e.Opts.Config.DefaultProvider = provider
		if e.Opts.AgentDir != "" {
			if fn := boundStream(e.Opts.AgentDir, provider); fn != nil {
				e.Stream = fn
			}
		}
	}
	if id != "" {
		e.Opts.Config.Model = id
		e.Opts.Config.DefaultModel = id
	}
	if thinking != "" {
		e.Opts.Config.Thinking = thinking
	}
}

// PrintText streams a prompt to out as plain text (--mode text / --print).
func (e *Engine) PrintText(ctx context.Context, out io.Writer, history []ai.Message, user string) error {
	stream := e.RunPrompt(ctx, history, user, nil)
	var last []agent.Msg
	for ev := range stream.Events() {
		switch ev.Type {
		case agent.EventMessageUpdate:
			if ev.AIEvent != nil && ev.AIEvent.Type == ai.EventTextDelta {
				_, _ = io.WriteString(out, ev.AIEvent.Delta)
			}
		case agent.EventToolStart:
			fmt.Fprintf(out, "\n⚙ %s\n", ev.ToolName)
		case agent.EventAgentEnd:
			last = ev.Messages
		}
	}
	fmt.Fprintln(out)
	e.PersistTranscript(last)
	return nil
}

// PrintJSON writes NDJSON agent events (--mode json).
func (e *Engine) PrintJSON(ctx context.Context, out io.Writer, history []ai.Message, user string, images []ai.ImageContent) error {
	enc := json.NewEncoder(out)
	write := e.onSessionEvent
	if write == nil {
		write = func(v any) { _ = enc.Encode(v) }
		prev := e.onSessionEvent
		e.onSessionEvent = write
		defer func() { e.onSessionEvent = prev }()
	}
	e.retryAttempt = 0
	hist := history
	continued := false
	prefixLen := 0
	for {
		var stream *agent.Stream
		if !continued {
			stream = e.RunPrompt(ctx, hist, user, images)
		} else {
			stream = e.runLoop(ctx, hist, nil)
		}
		var last []agent.Msg
		for ev := range stream.Events() {
			if ev.Type == agent.EventMessageEnd && ev.Assistant != nil &&
				ev.Assistant.StopReason != ai.StopError && e.retryAttempt > 0 {
				e.emitSession(map[string]any{
					"type":    "auto_retry_end",
					"success": true,
					"attempt": e.retryAttempt,
				})
				e.retryAttempt = 0
			}
			if ev.Type == agent.EventAgentEnd {
				ev.WillRetry = e.willRetryAfterAgentEnd(ev)
			}
			payload, err := agent.ToJSON(ev)
			if err != nil {
				return err
			}
			write(payload)
			if ev.Type == agent.EventAgentEnd {
				last = ev.Messages
			}
		}
		if continued {
			if prefixLen > len(last) {
				prefixLen = len(last)
			}
			e.persisted = 0
			e.PersistTranscript(last[prefixLen:])
		} else {
			e.PersistTranscript(last)
		}
		if !e.prepareRetry(ctx, last) {
			break
		}
		hist = stripLastAssistant(last)
		prefixLen = len(hist)
		continued = true
	}
	write(map[string]any{"type": "agent_settled"})
	return nil
}

func boundStream(agentDir, provider string) ai.StreamFn {
	if provider == "" {
		return nil
	}
	p, ok := auth.Lookup(provider)
	if !ok {
		return nil
	}
	store := auth.Open(agentDir)
	return func(ctx context.Context, reqCtx ai.Context, opts ai.Options) (*ai.EventStream, error) {
		res, err := auth.Resolve(ctx, store, p, auth.ResolveOpts{})
		if err != nil {
			return ai.StreamWithAuth(provider, "", "", nil)(ctx, reqCtx, opts)
		}
		if res == nil {
			key := auth.APIKey(agentDir, provider)
			if key == "" {
				return ai.EchoStreamFn()(ctx, reqCtx, opts)
			}
			return ai.StreamWithAuth(provider, key, "", nil)(ctx, reqCtx, opts)
		}
		key := res.Auth.APIKey
		if key == "" {
			key = auth.Secret(res)
		}
		if key == "" && len(res.Auth.Headers) == 0 {
			return ai.EchoStreamFn()(ctx, reqCtx, opts)
		}
		return ai.StreamWithAuth(provider, key, res.Auth.BaseURL, res.Auth.Headers)(ctx, reqCtx, opts)
	}
}
