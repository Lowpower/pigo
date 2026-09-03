package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/migrate"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/sandbox"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/shell"
	"github.com/Lowpower/pigo/internal/trust"
	"github.com/Lowpower/pigo/internal/tui"
	"github.com/Lowpower/pigo/internal/version"
)

func main() {
	expanded := expandShortFlags(os.Args[1:])
	rest, unknown := peelUnknownFlags(expanded)
	extraFlags = unknown
	os.Args = append([]string{os.Args[0]}, rest...)
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

type cliFlags struct {
	print           bool
	mode            string
	prompt          string
	configDir       string
	continueSession bool
	resume          bool
	sessionPath     string
	noSession       bool
	provider        string
	model           string
	thinking        string
	systemPrompt    string
	appendSystem    []string
	noContextFiles  bool
	noSkills        bool
	skills          []string
	noTools         bool
	tools           string
	excludeTools    string
	extension       []string
	noExtensions    bool
	theme           string
	themePaths      []string
	noThemes        bool
	listModels      bool
	listModelsQuery string
	offline         bool
	export          string
	fork            string
	sessionID       string
	name            string
	apiKey          string
	noBuiltinTools  bool
	modelsFlag      string
	promptTemplates []string
	noPromptTpls    bool
	tuiMode         string
	noSandbox       bool
	approve         bool
	noApprove       bool
	sessionDir      string
	verbose         bool
}

func newRootCmd() *cobra.Command {
	var f cliFlags

	cmd := &cobra.Command{
		Use:   "pigo [prompt...]",
		Short: "pigo — a coding agent",
		Long: `pigo — a coding agent

Environment:
  PIGO_CODING_AGENT_DIR           Agent config directory
  PIGO_CODING_AGENT_SESSION_DIR   Session storage directory (overridden by --session-dir)
  PIGO_TELEMETRY                  Override install telemetry (1/true/yes or 0/false/no)
  PIGO_OFFLINE                    Skip network at startup (also set by --offline)
  PIGO_SHARE_VIEWER_URL           Base URL for /share viewer
  PIGO_OAUTH_CALLBACK_HOST        OAuth callback bind host (default 127.0.0.1)
  PIGO_SERVER_LISTEN              Default unix:// address for pigo server --listen
  PIGO_SERVER_CONNECT             Default unix:// address for pigo client --connect
`,
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoot(cmd, args, f)
		},
	}

	cmd.Flags().BoolVarP(&f.print, "print", "p", false, "run non-interactively")
	cmd.Flags().StringVar(&f.mode, "mode", "", "output mode: text|json|rpc (default: interactive TTY, else text)")
	cmd.Flags().StringVar(&f.prompt, "prompt", "", "prompt text (alias of positional args)")
	cmd.Flags().StringVar(&f.configDir, "config-dir", "", "agent dir (default ~/.pigo/agent; env PIGO_CODING_AGENT_DIR)")
	cmd.Flags().BoolVarP(&f.continueSession, "continue", "c", false, "continue the most recent session in this directory")
	cmd.Flags().BoolVarP(&f.resume, "resume", "r", false, "resume a session (opens picker in interactive mode)")
	cmd.Flags().StringVar(&f.sessionPath, "session", "", "session file path or id")
	cmd.Flags().BoolVar(&f.noSession, "no-session", false, "do not persist a session")
	cmd.Flags().StringVar(&f.provider, "provider", "", "provider id")
	cmd.Flags().StringVar(&f.model, "model", "", "model id")
	cmd.Flags().StringVar(&f.thinking, "thinking", "", "thinking level: off|minimal|low|medium|high|xhigh|max")
	cmd.Flags().StringVar(&f.systemPrompt, "system-prompt", "", "replace the default system prompt (text or file path)")
	cmd.Flags().StringArrayVar(&f.appendSystem, "append-system-prompt", nil, "append text or file contents to the system prompt (repeatable)")
	cmd.Flags().BoolVarP(&f.noContextFiles, "no-context-files", "", false, "skip AGENTS.md / CLAUDE.md")
	cmd.Flags().BoolVar(&f.noSkills, "no-skills", false, "disable skill discovery")
	cmd.Flags().StringArrayVar(&f.skills, "skill", nil, "extra skill path (repeatable)")
	cmd.Flags().BoolVar(&f.noTools, "no-tools", false, "disable all tools")
	cmd.Flags().StringVarP(&f.tools, "tools", "t", "", "comma-separated tool allowlist")
	cmd.Flags().StringVar(&f.excludeTools, "exclude-tools", "", "comma-separated tool denylist")
	cmd.Flags().StringArrayVarP(&f.extension, "extension", "e", nil, "extension command to spawn (repeatable)")
	cmd.Flags().BoolVar(&f.noExtensions, "no-extensions", false, "skip extension auto-discovery (explicit -e still loads)")
	cmd.Flags().StringVar(&f.theme, "use-theme", "", "theme name for this run (does not write settings)")
	cmd.Flags().StringArrayVar(&f.themePaths, "theme", nil, "load a theme file or directory (repeatable)")
	cmd.Flags().BoolVar(&f.noThemes, "no-themes", false, "disable theme discovery")
	cmd.Flags().BoolVar(&f.listModels, "list-models", false, "list known models and exit")
	cmd.Flags().StringVar(&f.listModelsQuery, "list-models-query", "", "filter --list-models")
	cmd.Flags().BoolVar(&f.offline, "offline", false, "skip network at startup (sets PIGO_OFFLINE=1)")
	cmd.Flags().StringVar(&f.export, "export", "", "export a session JSONL to HTML and exit")
	cmd.Flags().StringVar(&f.fork, "fork", "", "fork session file or id into a new session")
	cmd.Flags().StringVar(&f.sessionID, "session-id", "", "resume session by id prefix")
	cmd.Flags().StringVarP(&f.name, "name", "n", "", "set session display name")
	cmd.Flags().StringVar(&f.apiKey, "api-key", "", "API key for this process (does not persist)")
	cmd.Flags().BoolVar(&f.noBuiltinTools, "no-builtin-tools", false, "disable built-in tools (extensions still load)")
	cmd.Flags().StringVar(&f.modelsFlag, "models", "", "comma-separated model patterns for Ctrl+P cycling")
	cmd.Flags().StringArrayVar(&f.promptTemplates, "prompt-template", nil, "load a prompt template file or directory")
	cmd.Flags().BoolVar(&f.noPromptTpls, "no-prompt-templates", false, "disable prompt template discovery")
	cmd.Flags().StringVar(&f.tuiMode, "tui-mode", "", "TUI layout: regular|fullscreen")
	cmd.Flags().BoolVar(&f.noSandbox, "no-sandbox", false, "disable OS-level sandbox wrapping for bash")
	cmd.Flags().BoolVarP(&f.approve, "approve", "a", false, "trust project-local files for this run")
	cmd.Flags().BoolVar(&f.noApprove, "no-approve", false, "ignore project-local files for this run")
	cmd.Flags().StringVar(&f.sessionDir, "session-dir", "", "directory for session storage and lookup")
	cmd.Flags().BoolVar(&f.verbose, "verbose", false, "force verbose startup (overrides quietStartup)")
	cmd.Flags().BoolP("version", "v", false, "print version and exit")

	cmd.AddCommand(newAuthCmd(), newConfigCmd(), newServerCmd(), newClientCmd())
	addPackageCommands(cmd)
	return cmd
}

func runRoot(cmd *cobra.Command, args []string, f cliFlags) error {
	if v, _ := cmd.Flags().GetBool("version"); v {
		fmt.Fprintf(cmd.OutOrStdout(), "pigo %s\n", version.Version)
		return nil
	}
	if f.offline {
		_ = os.Setenv("PIGO_OFFLINE", "1")
	}
	agentDir := f.configDir
	if agentDir == "" {
		agentDir = config.DefaultConfigDir()
	}
	sandbox.SetAgentDir(agentDir)
	sandbox.SetNoSandbox(f.noSandbox)
	fileCfg, err := config.Load(agentDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := fileCfg
	shell.SetPath(cfg.ShellPath)
	if f.provider != "" {
		cfg.Provider, cfg.DefaultProvider = f.provider, f.provider
	}
	if f.model != "" {
		applyModelSpec(&cfg.DefaultProvider, &cfg.DefaultModel, &cfg.Thinking, f.model)
		cfg.Provider, cfg.Model = cfg.DefaultProvider, cfg.DefaultModel
	}
	if f.thinking != "" {
		cfg.Thinking = f.thinking
	}
	if f.theme != "" {
		cfg.Theme = f.theme
	}
	if f.tuiMode != "" {
		switch strings.ToLower(strings.TrimSpace(f.tuiMode)) {
		case "regular", "fullscreen":
			cfg.TUIMode = strings.ToLower(strings.TrimSpace(f.tuiMode))
		default:
			return fmt.Errorf("--tui-mode requires regular or fullscreen")
		}
	}
	if f.verbose {
		off := false
		cfg.QuietStartupFlag = &off
	}
	if f.apiKey != "" {
		switch cfg.ResolvedProvider() {
		case "openai":
			_ = os.Setenv("OPENAI_API_KEY", f.apiKey)
		case "opencode":
			_ = os.Setenv("OPENCODE_API_KEY", f.apiKey)
		case "google":
			_ = os.Setenv("GEMINI_API_KEY", f.apiKey)
		case "amazon-bedrock":
			_ = os.Setenv("AWS_BEARER_TOKEN_BEDROCK", f.apiKey)
		default:
			_ = os.Setenv("ANTHROPIC_API_KEY", f.apiKey)
		}
	}
	auth.ApplyEnv(agentDir)
	cwd, _ := os.Getwd()
	mig := migrate.Run(cwd, agentDir)
	for _, w := range mig.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", w)
	}
	offline := f.offline || os.Getenv("PIGO_OFFLINE") != ""
	catalogURL := ""
	if !offline {
		catalogURL = models.DefaultCatalogBaseURL()
	}
	if err := models.PrepareCatalog(agentDir, catalogURL, offline); err != nil {
		return fmt.Errorf("models.json: %w", err)
	}
	if f.listModels {
		q := f.listModelsQuery
		if q == "" && len(args) > 0 {
			q = args[0]
		}
		ids := auth.AuthenticatedIDs(auth.Open(agentDir))
		for _, m := range models.SearchIn(models.Available(ids), q) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s/%s\t%s\n", m.Provider, m.ID, m.API)
		}
		return nil
	}

	if f.export != "" {
		outPath := ""
		if len(args) > 0 {
			outPath = args[0]
		}
		path, err := session.ExportHTMLFileWith(f.export, session.HTMLOptions{
			OutputPath: outPath,
			ThemeName:  cfg.Theme,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Exported to: %s\n", path)
		return nil
	}

	var override *bool
	if f.approve {
		v := true
		override = &v
	} else if f.noApprove {
		v := false
		override = &v
	}
	st := trust.Open(agentDir)
	trusted := trust.Decide(st, cwd, trust.Options{Override: override, Default: fileCfg.ProjectTrustDefault()})
	cfg = config.ApplyProject(fileCfg, cwd, trusted)
	if f.provider != "" {
		cfg.Provider, cfg.DefaultProvider = f.provider, f.provider
	}
	if f.model != "" {
		applyModelSpec(&cfg.DefaultProvider, &cfg.DefaultModel, &cfg.Thinking, f.model)
		cfg.Provider, cfg.Model = cfg.DefaultProvider, cfg.DefaultModel
	}
	if f.thinking != "" {
		cfg.Thinking = f.thinking
	}
	if f.theme != "" {
		cfg.Theme = f.theme
	}
	if f.tuiMode != "" {
		switch strings.ToLower(strings.TrimSpace(f.tuiMode)) {
		case "regular", "fullscreen":
			cfg.TUIMode = strings.ToLower(strings.TrimSpace(f.tuiMode))
		}
	}
	if f.verbose {
		off := false
		cfg.QuietStartupFlag = &off
	}
	applyHTTPProxy(cfg.HTTPProxy)
	shell.SetPath(cfg.ShellPath)
	sandbox.SetProjectTrusted(trusted)
	userCfg := fileCfg

	sessionDir := resolveSessionDir(f.sessionDir, cfg.SessionDir, "")

	msgs, files := splitPromptArgs(args)
	if f.prompt != "" {
		msgs = append([]string{f.prompt}, msgs...)
	}
	fileText := ""
	if len(files) > 0 {
		inline, err := inlineFiles(cwd, files)
		if err != nil {
			return err
		}
		fileText = inline
	}

	mode := f.mode
	if mode == "" {
		if f.print || !isTTY() {
			mode = "text"
		} else {
			mode = "interactive"
		}
	}

	stdinContent := ""
	if mode != "rpc" {
		stdinContent = readPipedStdin()
		if stdinContent != "" && mode == "interactive" {
			mode = "text"
		}
	}
	prompt, restMsgs := buildInitialMessage(stdinContent, fileText, msgs)
	if f.mode == "" && mode == "interactive" && prompt != "" {
		mode = "text"
	}

	systemPrompt := resolvePromptInput(f.systemPrompt)
	appendSystem := make([]string, 0, len(f.appendSystem))
	for _, s := range f.appendSystem {
		appendSystem = append(appendSystem, resolvePromptInput(s))
	}

	var sess *session.Manager
	if !f.noSession {
		switch {
		case f.fork != "":
			src, err2 := session.FindByIDAt(cwd, agentDir, f.fork, sessionDir)
			if err2 != nil {
				src, err2 = session.Open(f.fork)
			}
			if err2 != nil {
				return fmt.Errorf("fork: %w", err2)
			}
			sess, err = src.Fork(cwd, agentDir)
			if err != nil {
				return fmt.Errorf("fork session: %w", err)
			}
		case f.sessionPath != "":
			sess, err = session.Open(f.sessionPath)
			if err != nil {
				sess, err = session.FindByIDAt(cwd, agentDir, f.sessionPath, sessionDir)
			}
			if err != nil {
				return fmt.Errorf("open session: %w", err)
			}
		case f.sessionID != "":
			sess, err = session.FindByIDAt(cwd, agentDir, f.sessionID, sessionDir)
			if err != nil {
				return fmt.Errorf("session-id: %w", err)
			}
		case f.continueSession:
			sess, err = session.ContinueRecentAt(cwd, agentDir, sessionDir)
			if err != nil {
				return fmt.Errorf("continue session: %w", err)
			}
		case f.resume:
			if tui.ShouldOpenResumePicker(mode, f.resume, f.sessionID, f.sessionPath, f.fork, f.noSession) {
				sess = nil
			} else {
				sess, err = session.ContinueRecentAt(cwd, agentDir, sessionDir)
				if err != nil {
					return fmt.Errorf("resume session: %w", err)
				}
			}
		default:
			sess = session.NewAt(cwd, agentDir, sessionDir)
		}
		if f.name != "" && sess != nil {
			sess.SetName(f.name)
		}
	}

	exts := f.extension

	cliProvider := f.provider
	cliModel := ""
	if f.model != "" {
		p, id, _ := models.ParseSpec(f.model)
		if cliProvider == "" {
			cliProvider = p
		}
		cliModel = id
	}

	eng, err := runtime.New(cmd.Context(), runtime.Options{
		Config:         cfg,
		UserConfig:     &userCfg,
		Cwd:            cwd,
		AgentDir:       agentDir,
		Session:        sess,
		SystemPrompt:   systemPrompt,
		AppendSystem:   appendSystem,
		NoContextFiles: f.noContextFiles,
		NoSkills:       f.noSkills,
		SkillPaths:     f.skills,
		NoTools:        f.noTools,
		NoBuiltinTools: f.noBuiltinTools,
		ToolAllow:      splitCSV(f.tools),
		ToolDeny:       splitCSV(f.excludeTools),
		CLIExtensions:  exts,
		NoExtensions:   f.noExtensions,
		ProjectTrusted: trusted,
		ContextWindow:  cfg.ContextWindow,
		NoPromptTpls:   f.noPromptTpls,
		PromptPaths:    f.promptTemplates,
		ThemePaths:     f.themePaths,
		NoThemes:       f.noThemes,
		Models:         splitCSV(f.modelsFlag),
		CLIProvider:    cliProvider,
		CLIModel:       cliModel,
		CLIThinking:    f.thinking,
		CatalogBaseURL: catalogURL,
		Offline:        f.offline,
		SessionDir:     sessionDir,
		UnknownFlags:   extraFlags,
		InputSource:    inputSource(mode),
	})
	if err != nil {
		return err
	}
	defer eng.Close()
	if leftover := eng.UnclaimedFlags(); len(leftover) > 0 {
		return fmt.Errorf("%s", formatUnclaimed(leftover))
	}

	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	history := eng.History()
	switch mode {
	case "interactive":
		if prompt != "" {
			if err := eng.PrintText(ctx, out, history, prompt); err != nil {
				return err
			}
		}
		if tui.ShouldOpenResumePicker(mode, f.resume, f.sessionID, f.sessionPath, f.fork, f.noSession) {
			return tui.RunEngineResumePicker(cfg, eng)
		}
		return tui.RunEngine(cfg, eng)
	case "text", "print":
		prompts := printPrompts(prompt, restMsgs)
		if len(prompts) == 0 {
			fmt.Fprintf(out, "provider=%s model=%s theme=%s\n", cfg.ResolvedProvider(), cfg.ResolvedModel(), cfg.Theme)
			return nil
		}
		hist := history
		for _, p := range prompts {
			if err := eng.PrintText(ctx, out, hist, p); err != nil {
				return err
			}
			hist = eng.History()
		}
		return nil
	case "json":
		prompts := printPrompts(prompt, restMsgs)
		if len(prompts) == 0 {
			return fmt.Errorf("--mode json requires a prompt")
		}
		if err := eng.WriteSessionHeader(out); err != nil {
			return err
		}
		hist := history
		for _, p := range prompts {
			if err := eng.PrintJSON(ctx, out, hist, p, nil); err != nil {
				return err
			}
			hist = eng.History()
		}
		return nil
	case "rpc":
		return eng.ServeRPC(ctx, cmd.InOrStdin(), out)
	default:
		return fmt.Errorf("unknown --mode %q (want: interactive|text|json|rpc)", mode)
	}
}

func printPrompts(initial string, rest []string) []string {
	var out []string
	if initial != "" {
		out = append(out, initial)
	}
	out = append(out, rest...)
	return out
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func authProviderFromFlags(cmd *cobra.Command) (string, error) {
	provider, _ := cmd.Flags().GetString("provider")
	model, _ := cmd.Flags().GetString("model")
	if provider == "" && model != "" {
		p, _, _ := models.ParseSpec(model)
		provider = p
	}
	if provider == "" {
		return "", fmt.Errorf("credential printing requires --provider <provider> or --model <model>")
	}
	return provider, nil
}

func isTTY() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "manage stored provider credentials"}
	login := &cobra.Command{
		Use:   "login <provider>",
		Short: "store an API key for a provider (anthropic|openai|opencode)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			fmt.Fprintf(cmd.ErrOrStderr(), "API key for %s: ", provider)
			key, err := readLine(cmd.InOrStdin())
			if err != nil {
				return err
			}
			return auth.SetAPIKey(config.DefaultConfigDir(), provider, strings.TrimSpace(key))
		},
	}
	logout := &cobra.Command{
		Use:   "logout <provider>",
		Short: "remove a stored API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return auth.Delete(config.DefaultConfigDir(), args[0])
		},
	}
	printKey := &cobra.Command{
		Use:   "print-api-key",
		Short: "print a stored API key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			provider, err := authProviderFromFlags(cmd)
			if err != nil {
				return err
			}
			key, err := auth.PrintSecret(cmd.Context(), config.DefaultConfigDir(), provider, auth.TypeAPIKey, 0)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), key)
			return nil
		},
	}
	printKey.Flags().String("provider", "", "provider id")
	printKey.Flags().String("model", "", "model spec (provider inferred)")
	printBearer := &cobra.Command{
		Use:   "print-bearer-token",
		Short: "print an OAuth bearer token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			provider, err := authProviderFromFlags(cmd)
			if err != nil {
				return err
			}
			raw, _ := cmd.Flags().GetString("min-expiry")
			d := auth.DefaultBearerMinExpiry
			if raw != "" {
				d, err = auth.ParseMinExpiry(raw)
				if err != nil {
					return err
				}
			}
			key, err := auth.PrintSecret(cmd.Context(), config.DefaultConfigDir(), provider, auth.TypeOAuth, d)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), key)
			return nil
		},
	}
	printBearer.Flags().String("provider", "", "provider id")
	printBearer.Flags().String("model", "", "model spec (provider inferred)")
	printBearer.Flags().String("min-expiry", "30m", "minimum remaining token validity")
	check := &cobra.Command{
		Use:   "check",
		Short: "check whether a provider is authenticated",
		RunE: func(cmd *cobra.Command, _ []string) error {
			provider, err := authProviderFromFlags(cmd)
			if err != nil {
				return fmt.Errorf("auth checks require --provider <provider> or --model <model>")
			}
			asJSON, _ := cmd.Flags().GetBool("json")
			showCreds, _ := cmd.Flags().GetBool("credentials")
			noRefresh, _ := cmd.Flags().GetBool("no-refresh")
			res := auth.CheckProvider(cmd.Context(), config.DefaultConfigDir(), provider, !noRefresh, showCreds)
			if asJSON {
				b, _ := json.Marshal(res)
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
			} else {
				if res.Status != "ready" {
					return fmt.Errorf("%s: %s", provider, res.Reason)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: ok (%s)\n", provider, res.AuthType)
				if showCreds && res.Credentials != "" {
					fmt.Fprintln(cmd.OutOrStdout(), res.Credentials)
				}
			}
			if res.Status != "ready" {
				return fmt.Errorf("%s: %s", provider, res.Reason)
			}
			return nil
		},
	}
	check.Flags().String("provider", "", "provider id")
	check.Flags().String("model", "", "model spec")
	check.Flags().Bool("json", false, "JSON output")
	check.Flags().Bool("credentials", false, "include the credential")
	check.Flags().Bool("no-refresh", false, "do not refresh OAuth tokens")
	cmd.AddCommand(login, logout, printKey, printBearer, check)
	return cmd
}

func newConfigCmd() *cobra.Command {
	var f packageFlags
	cmd := &cobra.Command{
		Use:   "config",
		Short: "enable or disable extensions, skills, prompts, and themes",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().Bool("print", false, "print resolved provider/model/theme")
	addPackageFlags(cmd, &f, true, false)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		printDump, _ := cmd.Flags().GetBool("print")
		if printDump {
			dir := config.DefaultConfigDir()
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "dir=%s\nprovider=%s\nmodel=%s\ntheme=%s\nthinking=%s\n",
				dir, cfg.ResolvedProvider(), cfg.ResolvedModel(), cfg.Theme, cfg.Thinking)
			return nil
		}
		m, err := openPackageManager(f)
		if err != nil {
			return err
		}
		if f.local && !m.Trusted {
			return fmt.Errorf("project is not trusted; use --approve to modify local resource config")
		}
		if !isTTY() {
			return fmt.Errorf("config editor requires a TTY (use --print to dump settings)")
		}
		return tui.RunConfigSelector(m, f.local)
	}
	return cmd
}

func readLine(r io.Reader) (string, error) {
	s := bufio.NewScanner(r)
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return s.Text(), nil
}

// silence unused context import in case of build tags; runRoot uses cmd.Context.
var _ = context.Background
