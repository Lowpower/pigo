package eval

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	Dir            string
	OutDir         string
	Provider       string
	Model          string
	APIKey         string
	RunsPerVariant int
	ContainerImage string
	Stream         ai.StreamFn // when set, skip credential checks (offline tests)
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

func resolveRuns(opts Options) int {
	if opts.RunsPerVariant > 0 {
		return opts.RunsPerVariant
	}
	if v := strings.TrimSpace(os.Getenv("PIGO_EVAL_RUNS_PER_VARIANT")); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			return n
		}
	}
	return 1
}

func resolveContainerImage(opts Options) string {
	if s := strings.TrimSpace(opts.ContainerImage); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("PIGO_CONTAINER_IMAGE"))
}

// RunDir loads scenarios from opts.Dir, runs them, and writes report.json.
func RunDir(ctx context.Context, opts Options) (Report, error) {
	scenarios, err := LoadDir(opts.Dir)
	if err != nil {
		return Report{}, err
	}
	provider := resolveProvider(opts)
	skipAll := opts.Stream == nil
	skipReason := "no API key"
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
	if resolveContainerImage(opts) != "" {
		if _, err := lookDocker("docker"); err != nil {
			skipAll = true
			skipReason = "docker not found in PATH"
		}
	}

	var results []Result
	for _, sc := range scenarios {
		if skipAll {
			results = append(results, Result{
				Name:   sc.Name,
				Status: StatusSkip,
				Error:  skipReason,
			})
			continue
		}
		results = append(results, runScenario(ctx, sc, opts, provider)...)
	}
	rep := summarize(results)
	if err := writeReport(opts.OutDir, rep); err != nil {
		return rep, err
	}
	return rep, nil
}

func runScenario(ctx context.Context, sc Scenario, opts Options, provider string) []Result {
	if !sc.DocsLift {
		return []Result{runOne(ctx, sc, opts, provider, "", 0, 1)}
	}
	runs := resolveRuns(opts)
	out := make([]Result, 0, 2*runs)
	for i := 1; i <= runs; i++ {
		out = append(out, runOne(ctx, sc, opts, provider, VariantWithoutDocs, i, runs))
		out = append(out, runOne(ctx, sc, opts, provider, VariantWithDocs, i, runs))
	}
	return out
}

func runOne(ctx context.Context, sc Scenario, opts Options, provider, variant string, repeat, runs int) Result {
	res := Result{Name: sc.Name, Variant: variant, Repeat: repeat}
	files := sc.Files
	noContext := sc.Files["AGENTS.md"] == "" && sc.Files["CLAUDE.md"] == ""
	if variant == VariantWithoutDocs {
		files = stripContextDocs(sc.Files)
		noContext = true
	}

	root, err := os.MkdirTemp("", "pigo-eval-")
	if err != nil {
		res.Status = StatusError
		res.Error = err.Error()
		return res
	}
	defer func() { _ = os.RemoveAll(root) }()

	cwd := filepath.Join(root, "cwd")
	agentDir := filepath.Join(root, "agent")
	home := filepath.Join(root, "home")
	for _, d := range []string{cwd, agentDir, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			res.Status = StatusError
			res.Error = err.Error()
			return res
		}
	}
	if err := seedFiles(cwd, files); err != nil {
		res.Status = StatusError
		res.Error = err.Error()
		return res
	}
	if opts.APIKey != "" {
		if err := auth.SetAPIKey(agentDir, provider, opts.APIKey); err != nil {
			res.Status = StatusError
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
		Container:       config.ContainerSettings{Image: resolveContainerImage(opts)},
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
		NoContextFiles: noContext,
		ProjectTrusted: true,
		CLIProvider:    provider,
		CLIModel:       resolveModel(opts),
		CLIThinking:    "off",
		Offline:        opts.Stream != nil,
		InputSource:    "cli",
	})
	if err != nil {
		res.Status = StatusError
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
		res.Status = StatusError
		res.Error = err.Error()
		res.Output = lastAssistantText(last)
		copySession(opts.OutDir, sc.Name, variant, repeat, runs, sess, &res)
		return res
	}
	res.Output = lastAssistantText(last)
	stats := session.CollectStats(sess, nil, 0)
	res.Tokens = stats.Tokens.Total
	res.Cost = stats.Cost
	copySession(opts.OutDir, sc.Name, variant, repeat, runs, sess, &res)
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

func sessionArtifactName(name, variant string, repeat, runs int) string {
	base := name
	if variant != "" && runs > 1 && repeat > 0 {
		base = fmt.Sprintf("%s-%d", name, repeat)
	}
	file := base + ".jsonl"
	if variant == "" {
		return file
	}
	return filepath.Join(variant, file)
}

func copySession(outDir, name, variant string, repeat, runs int, sess *session.Manager, res *Result) {
	if outDir == "" || sess == nil {
		return
	}
	src := sess.File()
	if src == "" {
		return
	}
	rel := sessionArtifactName(name, variant, repeat, runs)
	dst := filepath.Join(outDir, "sessions", rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
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
