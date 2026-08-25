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
	ToolAllow      []string
	ToolDeny       []string
	Extensions     []string // argv[0] of each extension binary
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

	mu     sync.Mutex
	steer  []ai.Message
	follow []ai.Message

	onSessionEvent func(any)
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

	sf, provider := ai.DefaultStreamFn()
	reg := tools.Default()
	if opts.NoTools {
		reg = tools.NewRegistry()
	} else {
		reg = filterTools(reg, opts.ToolAllow, opts.ToolDeny)
	}

	var hosts []*ext.Host
	for _, spec := range opts.Extensions {
		argv := strings.Fields(spec)
		if len(argv) == 0 {
			continue
		}
		h, err := ext.Spawn(ctx, argv[0], argv, ext.Options{})
		if err != nil {
			return nil, fmt.Errorf("extension %q: %w", spec, err)
		}
		hosts = append(hosts, h)
		for _, t := range h.Tools() {
			reg = tools.NewRegistry(append(reg.List(), wrapExt(h, t.Name, t.Description, t.Parameters))...)
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

func (e *Engine) emitSession(v any) {
	if e.onSessionEvent != nil {
		e.onSessionEvent(v)
	}
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

// PushSteer queues a user message for the in-flight turn (pi steer).
func (e *Engine) PushSteer(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	e.mu.Lock()
	e.steer = append(e.steer, ai.Message{Role: ai.RoleUser, Content: text})
	e.mu.Unlock()
	e.emitQueueUpdate()
}

// PushFollow queues a user message for after the current turn (pi followUp).
func (e *Engine) PushFollow(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	e.mu.Lock()
	e.follow = append(e.follow, ai.Message{Role: ai.RoleUser, Content: text})
	e.mu.Unlock()
	e.emitQueueUpdate()
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

// drainQueue matches pi PendingMessageQueue.drain: "all" empties the queue;
// "one-at-a-time" (default) returns only the oldest message.
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

// MaybeCompact runs compaction when the estimate exceeds the window (pi auto-compact).
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
func (e *Engine) RunPrompt(ctx context.Context, history []ai.Message, user string) *agent.Stream {
	userMsg := ai.Message{Role: ai.RoleUser, Content: user}
	history = append(append([]ai.Message(nil), history...), userMsg)
	compacted, _, err := e.MaybeCompact(ctx, history)
	if err == nil {
		history = compacted
	}
	req := ai.Context{System: e.System, Messages: history}
	if e.Tools != nil {
		req.Tools = e.Tools.AITools()
	}
	return agent.Run(ctx, e.Stream, req, e.Executor(), agent.Config{
		Model:           e.Opts.Config.ResolvedModel(),
		Thinking:        e.Opts.Config.Thinking,
		Steering:        e.Steering,
		FollowUp:        e.FollowUp,
		NewUserMessages: []ai.Message{userMsg},
	})
}

// History restores provider-facing messages from the attached session.
func (e *Engine) AdoptSession(s *session.Manager) {
	e.Opts.Session = s
	if s == nil {
		e.persisted = 0
		return
	}
	e.persisted = len(s.Entries())
}

func (e *Engine) History() []ai.Message {
	if e.Opts.Session == nil {
		return nil
	}
	msgs := session.RestoreAIMessages(e.Opts.Session.Entries())
	e.persisted = len(msgs)
	return msgs
}

// Reload rediscovers skills and rebuilds the system prompt (pi /reload).
func (e *Engine) Reload() {
	if e.Opts.NoSkills {
		e.Skills = nil
	} else {
		e.Skills, _ = skills.Discover(e.Opts.Cwd, e.Opts.AgentDir, e.Opts.SkillPaths, true)
	}
	e.Templates = prompt.DiscoverTemplates(e.Opts.Cwd, e.Opts.AgentDir, e.Opts.PromptPaths, !e.Opts.NoPromptTpls)
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
			entry, err = e.Opts.Session.AppendMessage("user", map[string]any{"role": "user", "content": msg.Text})
		}
		if err == nil && entry != nil {
			e.emitSession(map[string]any{"type": "entry_appended", "entry": entry})
		}
	}
	e.persisted = len(msgs)
}

// CycleModel steps through --models or the catalog (pi cycle_model / ctrl+p).
func (e *Engine) CycleModel(backward bool) (models.Spec, bool) {
	next, ok := models.Cycle(e.Opts.Config.ResolvedProvider(), e.Opts.Config.ResolvedModel(), e.Scoped, backward)
	if !ok {
		return models.Spec{}, false
	}
	e.ApplyModel(next.Provider, next.ID, next.Thinking)
	return next, true
}

// CycleThinking steps thinking levels (pi cycle_thinking_level / shift+tab).
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
	}
	if id != "" {
		e.Opts.Config.Model = id
		e.Opts.Config.DefaultModel = id
	}
	if thinking != "" {
		e.Opts.Config.Thinking = thinking
	}
}

// PrintText streams a prompt to out as plain text (pi --mode text / --print).
func (e *Engine) PrintText(ctx context.Context, out io.Writer, history []ai.Message, user string) error {
	stream := e.RunPrompt(ctx, history, user)
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

// PrintJSON writes NDJSON agent events using pi's toJsonEvent shape.
func (e *Engine) PrintJSON(ctx context.Context, out io.Writer, history []ai.Message, user string) error {
	enc := json.NewEncoder(out)
	write := e.onSessionEvent
	if write == nil {
		write = func(v any) { _ = enc.Encode(v) }
	}
	stream := e.RunPrompt(ctx, history, user)
	var last []agent.Msg
	for ev := range stream.Events() {
		payload, err := agent.ToJSON(ev)
		if err != nil {
			return err
		}
		write(payload)
		if ev.Type == agent.EventAgentEnd {
			last = ev.Messages
		}
	}
	e.PersistTranscript(last)
	write(map[string]any{"type": "agent_settled"})
	return nil
}
