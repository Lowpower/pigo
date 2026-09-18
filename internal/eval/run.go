package eval

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
)

// Options configure a suite or single-case run.
type Options struct {
	Dir      string
	OutDir   string
	Provider string
	Model    string
	APIKey   string
	Stream   ai.StreamFn // when set, skip credential checks (offline tests)
}

func seedFiles(cwd string, files map[string]string) error {
	for rel, content := range files {
		if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			return fmt.Errorf("invalid file path %q", rel)
		}
		path := filepath.Join(cwd, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func resolveProvider(opts Options) string {
	if opts.Provider != "" {
		return opts.Provider
	}
	if p := strings.TrimSpace(os.Getenv("PIGO_PROVIDER")); p != "" {
		return p
	}
	return "anthropic"
}

func resolveModel(opts Options) string {
	if opts.Model != "" {
		return opts.Model
	}
	return strings.TrimSpace(os.Getenv("PIGO_MODEL"))
}

// RunDir loads scenarios from opts.Dir, runs them, and writes report.json.
func RunDir(ctx context.Context, opts Options) (Report, error) {
	scenarios, err := LoadDir(opts.Dir)
	if err != nil {
		return Report{}, err
	}
	provider := resolveProvider(opts)
	skipAll := opts.Stream == nil
	if skipAll {
		probe, err := os.MkdirTemp("", "pigo-eval-auth-")
		if err != nil {
			return Report{}, err
		}
		defer func() { _ = os.RemoveAll(probe) }()
		if opts.APIKey != "" {
			if err := auth.SetAPIKey(probe, provider, opts.APIKey); err != nil {
				return Report{}, err
			}
		}
		skipAll = !HasCredential(probe, provider)
	}

	var results []Result
	for _, sc := range scenarios {
		if skipAll {
			results = append(results, Result{
				Name:   sc.Name,
				Status: StatusSkip,
				Error:  "no API key",
			})
			continue
		}
		results = append(results, runOne(ctx, sc, opts, provider))
	}
	rep := summarize(results)
	if err := writeReport(opts.OutDir, rep); err != nil {
		return rep, err
	}
	return rep, nil
}

func runOne(ctx context.Context, sc Scenario, opts Options, provider string) Result {
	res := Result{Name: sc.Name}
	root, err := os.MkdirTemp("", "pigo-eval-")
	if err != nil {
		res.Status = StatusFail
		res.Error = err.Error()
		return res
	}
	defer func() { _ = os.RemoveAll(root) }()

	cwd := filepath.Join(root, "cwd")
	agentDir := filepath.Join(root, "agent")
	home := filepath.Join(root, "home")
	for _, d := range []string{cwd, agentDir, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			res.Status = StatusFail
			res.Error = err.Error()
			return res
		}
	}
	if err := seedFiles(cwd, sc.Files); err != nil {
		res.Status = StatusFail
		res.Error = err.Error()
		return res
	}
	if opts.APIKey != "" {
		if err := auth.SetAPIKey(agentDir, provider, opts.APIKey); err != nil {
			res.Status = StatusFail
			res.Error = err.Error()
			return res
		}
	}

	oldHome, oldProfile, oldTel := os.Getenv("HOME"), os.Getenv("USERPROFILE"), os.Getenv("PIGO_TELEMETRY")
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)
	_ = os.Setenv("PIGO_TELEMETRY", "0")
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		_ = os.Setenv("USERPROFILE", oldProfile)
		_ = os.Setenv("PIGO_TELEMETRY", oldTel)
	}()

	sess := session.New(cwd, agentDir)
	cfg := config.Config{
		Provider:        provider,
		DefaultProvider: provider,
		Model:           resolveModel(opts),
		DefaultModel:    resolveModel(opts),
		Thinking:        "off",
	}
	eng, err := runtime.New(ctx, runtime.Options{
		Config:         cfg,
		Cwd:            cwd,
		AgentDir:       agentDir,
		Session:        sess,
		SystemPrompt:   sc.SystemPrompt,
		NoTools:        sc.NoTools,
		ToolAllow:      sc.Tools,
		ToolDeny:       sc.ExcludeTools,
		NoExtensions:   true,
		NoSkills:       true,
		NoPromptTpls:   true,
		NoThemes:       true,
		NoContextFiles: sc.Files["AGENTS.md"] == "" && sc.Files["CLAUDE.md"] == "",
		ProjectTrusted: true,
		CLIProvider:    provider,
		CLIModel:       resolveModel(opts),
		CLIThinking:    "off",
		Offline:        opts.Stream != nil,
		InputSource:    "cli",
	})
	if err != nil {
		res.Status = StatusFail
		res.Error = err.Error()
		return res
	}
	defer eng.Close()
	if opts.Stream != nil {
		eng.Stream = opts.Stream
	}

	start := time.Now()
	stream := eng.RunPrompt(ctx, eng.History(), sc.Prompt, nil)
	var last []agent.Msg
	for ev := range stream.Events() {
		if ev.Type == agent.EventAgentEnd {
			last = ev.Messages
		}
	}
	res.Latency = time.Since(start)
	res.LatencyMs = res.Latency.Milliseconds()
	eng.PersistTranscript(last)
	if err := stopErr(last); err != nil {
		res.Status = StatusFail
		res.Error = err.Error()
		res.Output = lastAssistantText(last)
		copySession(opts.OutDir, sc.Name, sess, &res)
		return res
	}
	res.Output = lastAssistantText(last)
	stats := session.CollectStats(sess, nil, 0)
	res.Tokens = stats.Tokens.Total
	copySession(opts.OutDir, sc.Name, sess, &res)
	if err := grade(res.Output, sc.Expect); err != nil {
		res.Status = StatusFail
		res.Error = err.Error()
		return res
	}
	res.Status = StatusPass
	return res
}

func lastAssistantText(msgs []agent.Msg) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Assistant != nil {
			return msgs[i].Assistant.Text()
		}
	}
	return ""
}

func stopErr(last []agent.Msg) error {
	for i := len(last) - 1; i >= 0; i-- {
		a := last[i].Assistant
		if a == nil {
			continue
		}
		if a.StopReason == ai.StopError || a.StopReason == ai.StopAborted {
			msg := strings.TrimSpace(a.ErrorMessage)
			if msg == "" {
				msg = "request " + string(a.StopReason)
			}
			return fmt.Errorf("%s", msg)
		}
		return nil
	}
	return nil
}

func copySession(outDir, name string, sess *session.Manager, res *Result) {
	if outDir == "" || sess == nil {
		return
	}
	src := sess.File()
	if src == "" {
		return
	}
	dir := filepath.Join(outDir, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	dst := filepath.Join(dir, name+".jsonl")
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return
	}
	res.Session = dst
}
