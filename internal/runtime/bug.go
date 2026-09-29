package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/bugreport"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/pkgmgr"
	"github.com/Lowpower/pigo/internal/session"
)

func hostPaths(hosts []*ext.Host) []string {
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h != nil {
			out = append(out, h.Name())
		}
	}
	return out
}

func (e *Engine) setBugExtensions(paths, specs []string, rs []pkgmgr.Resource) {
	if e == nil {
		return
	}
	refs := make([]bugreport.ResourceRef, 0, len(rs))
	for _, r := range rs {
		if r.Type != "" && r.Type != pkgmgr.KindExtensions {
			continue
		}
		refs = append(refs, bugreport.ResourceRef{
			Path: r.Path, Source: r.Source, Scope: r.Scope, Origin: r.Origin, BaseDir: r.BaseDir,
		})
	}
	e.bugExts = bugreport.DescribeExtensions(paths, specs, refs)
}

// LoadedExtensions returns extensions included in /bug reports.
func (e *Engine) LoadedExtensions() []bugreport.Extension {
	if e == nil {
		return nil
	}
	return append([]bugreport.Extension(nil), e.bugExts...)
}

// BugInput collects the current process snapshot for /bug.
func (e *Engine) BugInput(hint string, transcript bool, summary string) bugreport.Input {
	in := bugreport.Input{
		Hint:           hint,
		Summary:        summary,
		ThinkingLevel:  e.Opts.Config.Thinking,
		Model:          e.bugModel(),
		Provider:       e.bugProvider(),
		Extensions:     e.LoadedExtensions(),
		IncludeProject: e.Opts.ProjectTrusted,
		GlobalSettings: readRaw(filepath.Join(e.Opts.AgentDir, "settings.json")),
	}
	if e.Opts.ProjectTrusted {
		in.ProjectSettings = readRaw(filepath.Join(configProjectSettings(e.Opts.Cwd)))
	}
	if e.Opts.Session != nil {
		h := e.Opts.Session.Header()
		in.Header = &h
		in.SessionID = e.Opts.Session.ID()
		in.Branch = e.Opts.Session.GetBranch("")
		in.MessageCount = countBugMessages(in.Branch)
		in.IncludeSession = transcript
	}
	in.Crashes = bugreport.ReadCrashLog(e.Opts.AgentDir)
	return in
}

func configProjectSettings(cwd string) string {
	return filepath.Join(cwd, ".pigo", "settings.json")
}

func readRaw(path string) json.RawMessage {
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	return b
}

func countBugMessages(entries []session.Entry) int {
	n := 0
	for _, e := range entries {
		if e.Type == "message" || e.Type == "" {
			n++
		}
	}
	return n
}

func (e *Engine) bugModel() *bugreport.ModelInfo {
	m := e.currentModel()
	if m.Provider == "" && m.ID == "" {
		return nil
	}
	info := &bugreport.ModelInfo{
		Provider:      m.Provider,
		ID:            m.ID,
		Name:          m.Name,
		API:           m.API,
		BaseURL:       m.BaseURL,
		Reasoning:     m.SupportsReasoning(),
		Input:         append([]string(nil), m.Input...),
		ContextWindow: m.ContextWindow,
		MaxTokens:     m.MaxTokens,
	}
	if info.BaseURL == "" {
		if spec, ok := models.LookupProvider(m.Provider); ok {
			info.BaseURL = spec.BaseURL
		}
	}
	return info
}

func (e *Engine) bugProvider() *bugreport.ProviderInfo {
	id := e.Provider
	if id == "" {
		id = e.Opts.Config.ResolvedProvider()
	}
	if id == "" {
		return nil
	}
	info := &bugreport.ProviderInfo{ID: id, HeaderNames: []string{}, AuthTypes: []string{}}
	if spec, ok := models.LookupProvider(id); ok {
		info.Name = spec.Name
		info.BaseURL = spec.BaseURL
		for k := range spec.Headers {
			info.HeaderNames = append(info.HeaderNames, k)
		}
		sort.Strings(info.HeaderNames)
	}
	if p, ok := auth.Lookup(id); ok {
		if p.APIKey != nil {
			info.AuthTypes = append(info.AuthTypes, auth.TypeAPIKey)
		}
		if p.OAuth != nil {
			info.AuthTypes = append(info.AuthTypes, auth.TypeOAuth)
		}
	}
	if chk := auth.CheckAuth(auth.Open(e.Opts.AgentDir), id); chk != nil {
		info.AuthStatus = chk.Type
		info.UsingOAuth = chk.Type == auth.TypeOAuth
	}
	if e.extProviderIDs[id] {
		info.RegisteredByExtension = true
	}
	return info
}

// SummarizeBugReport asks the current model for a bug-report summary.
func (e *Engine) SummarizeBugReport(ctx context.Context, fallback []ai.Message, hint string) (string, error) {
	if e == nil || e.Stream == nil {
		return "", errors.New("bug report summary requires a model")
	}
	msgs := fallback
	if e.Opts.Session != nil {
		if restored := session.ModelMessages(e.Opts.Session.GetBranch("")); len(restored) > 0 {
			msgs = restored
		}
	}
	if len(msgs) == 0 {
		return "", errors.New("no messages to summarize")
	}
	selected := bugreport.SelectMessages(msgs, e.contextWindow()*6/10)
	stream, err := e.Stream(ctx, ai.Context{
		System: bugreport.SummarySystemPrompt(),
		Messages: []ai.Message{{
			Role:    ai.RoleUser,
			Content: bugreport.SummaryUserPrompt(selected, hint, len(msgs)),
		}},
	}, ai.Options{
		Model:      e.Opts.Config.ResolvedModel(),
		MaxTokens:  4096,
		Thinking:   e.Opts.Config.Thinking,
		SessionID:  e.sessionID(),
		Provider:   e.Provider,
		ToolChoice: "none",
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", bugreport.ErrCancelled
		}
		return "", err
	}
	_, final := stream.Collect()
	if ctx.Err() != nil || (final != nil && final.StopReason == ai.StopAborted) {
		return "", bugreport.ErrCancelled
	}
	if final == nil {
		return "", errors.New("bug report summary was empty")
	}
	if final.StopReason == ai.StopError {
		msg := final.ErrorMessage
		if msg == "" {
			msg = "bug report summary failed"
		}
		return "", errors.New(msg)
	}
	if len(final.ToolCalls()) > 0 {
		return "", errors.New("bug report summary attempted to call a tool")
	}
	text := strings.TrimSpace(final.Text())
	if text == "" {
		return "", errors.New("bug report summary was empty")
	}
	return text, nil
}
