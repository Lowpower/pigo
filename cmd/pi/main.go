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
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tui"
)

var version = "0.0.1-dev"

func main() {
	os.Args = append([]string{os.Args[0]}, expandShortFlags(os.Args[1:])...)
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
}

func newRootCmd() *cobra.Command {
	var f cliFlags

	cmd := &cobra.Command{
		Use:          "pi [prompt...]",
		Short:        "pigo — a coding agent",
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoot(cmd, args, f)
		},
	}

	cmd.Flags().BoolVarP(&f.print, "print", "p", false, "run non-interactively")
	cmd.Flags().StringVar(&f.mode, "mode", "", "output mode: text|json|rpc (default: interactive TTY, else text)")
	cmd.Flags().StringVar(&f.prompt, "prompt", "", "prompt text (alias of positional args)")
	cmd.Flags().StringVar(&f.configDir, "config-dir", "", "agent dir (default ~/.pi/agent; env PI_CODING_AGENT_DIR)")
	cmd.Flags().BoolVarP(&f.continueSession, "continue", "c", false, "continue the most recent session in this directory")
	cmd.Flags().BoolVarP(&f.resume, "resume", "r", false, "resume a session (most recent if --session omitted)")
	cmd.Flags().StringVar(&f.sessionPath, "session", "", "session file path or id")
	cmd.Flags().BoolVar(&f.noSession, "no-session", false, "do not persist a session")
	cmd.Flags().StringVar(&f.provider, "provider", "", "provider id")
	cmd.Flags().StringVar(&f.model, "model", "", "model id")
	cmd.Flags().StringVar(&f.thinking, "thinking", "", "thinking level: off|minimal|low|medium|high|xhigh|max")
	cmd.Flags().StringVar(&f.systemPrompt, "system-prompt", "", "replace the default system prompt")
	cmd.Flags().StringArrayVar(&f.appendSystem, "append-system-prompt", nil, "append text to the system prompt (repeatable)")
	cmd.Flags().BoolVarP(&f.noContextFiles, "no-context-files", "", false, "skip AGENTS.md / CLAUDE.md")
	cmd.Flags().BoolVar(&f.noSkills, "no-skills", false, "disable skill discovery")
	cmd.Flags().StringArrayVar(&f.skills, "skill", nil, "extra skill path (repeatable)")
	cmd.Flags().BoolVar(&f.noTools, "no-tools", false, "disable all tools")
	cmd.Flags().StringVarP(&f.tools, "tools", "t", "", "comma-separated tool allowlist")
	cmd.Flags().StringVar(&f.excludeTools, "exclude-tools", "", "comma-separated tool denylist")
	cmd.Flags().StringArrayVarP(&f.extension, "extension", "e", nil, "extension command to spawn (repeatable)")
	cmd.Flags().BoolVar(&f.noExtensions, "no-extensions", false, "do not load extensions")
	cmd.Flags().StringVar(&f.theme, "use-theme", "", "theme name")
	cmd.Flags().BoolVar(&f.listModels, "list-models", false, "list known models and exit")
	cmd.Flags().StringVar(&f.listModelsQuery, "list-models-query", "", "filter --list-models")
	cmd.Flags().BoolVar(&f.offline, "offline", false, "skip network at startup (sets PI_OFFLINE=1)")
	cmd.Flags().StringVar(&f.export, "export", "", "export a session JSONL to HTML and exit")
	cmd.Flags().StringVar(&f.fork, "fork", "", "fork session file or id into a new session")
	cmd.Flags().StringVar(&f.sessionID, "session-id", "", "resume session by id prefix")
	cmd.Flags().StringVarP(&f.name, "name", "n", "", "set session display name")
	cmd.Flags().StringVar(&f.apiKey, "api-key", "", "API key for this process (does not persist)")
	cmd.Flags().BoolVar(&f.noBuiltinTools, "no-builtin-tools", false, "disable built-in tools (extensions still load)")
	cmd.Flags().StringVar(&f.modelsFlag, "models", "", "comma-separated model patterns for Ctrl+P cycling")
	cmd.Flags().StringArrayVar(&f.promptTemplates, "prompt-template", nil, "load a prompt template file or directory")
	cmd.Flags().BoolVar(&f.noPromptTpls, "no-prompt-templates", false, "disable prompt template discovery")
	cmd.Flags().BoolP("version", "v", false, "print version and exit")

	cmd.AddCommand(newAuthCmd(), newConfigCmd())
	return cmd
}

func runRoot(cmd *cobra.Command, args []string, f cliFlags) error {
	if v, _ := cmd.Flags().GetBool("version"); v {
		fmt.Fprintf(cmd.OutOrStdout(), "pigo %s\n", version)
		return nil
	}
	if f.offline {
		_ = os.Setenv("PI_OFFLINE", "1")
	}
	agentDir := f.configDir
	if agentDir == "" {
		agentDir = config.DefaultConfigDir()
	}
	cfg, err := config.Load(agentDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
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
	if f.apiKey != "" {
		switch cfg.ResolvedProvider() {
		case "openai":
			_ = os.Setenv("OPENAI_API_KEY", f.apiKey)
		case "opencode":
			_ = os.Setenv("OPENCODE_API_KEY", f.apiKey)
		default:
			_ = os.Setenv("ANTHROPIC_API_KEY", f.apiKey)
		}
	}
	if f.noBuiltinTools {
		f.noTools = true
	}

	if f.listModels {
		q := f.listModelsQuery
		if q == "" && len(args) > 0 {
			q = args[0]
		}
		for _, m := range models.Search(q) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s/%s\t%s\n", m.Provider, m.ID, m.API)
		}
		return nil
	}

	cwd, _ := os.Getwd()
	if f.export != "" {
		outPath := ""
		if len(args) > 0 {
			outPath = args[0]
		}
		path, err := session.ExportHTMLFile(f.export, outPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Exported to: %s\n", path)
		return nil
	}

	msgs, files := splitPromptArgs(args)
	prompt := strings.TrimSpace(strings.Join(msgs, " "))
	if f.prompt != "" {
		prompt = f.prompt
	}
	if len(files) > 0 {
		inline, err := inlineFiles(cwd, files)
		if err != nil {
			return err
		}
		if prompt == "" {
			prompt = inline
		} else {
			prompt = inline + "\n" + prompt
		}
	}

	mode := f.mode
	if mode == "" {
		if f.print || prompt != "" || !isTTY() {
			mode = "text"
		} else {
			mode = "interactive"
		}
	}

	var sess *session.Manager
	if !f.noSession {
		switch {
		case f.fork != "":
			src, err2 := session.FindByID(cwd, agentDir, f.fork)
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
				sess, err = session.FindByID(cwd, agentDir, f.sessionPath)
			}
			if err != nil {
				return fmt.Errorf("open session: %w", err)
			}
		case f.sessionID != "":
			sess, err = session.FindByID(cwd, agentDir, f.sessionID)
			if err != nil {
				return fmt.Errorf("session-id: %w", err)
			}
		case f.continueSession || f.resume:
			sess, err = session.ContinueRecent(cwd, agentDir)
			if err != nil {
				return fmt.Errorf("resume session: %w", err)
			}
		default:
			sess = session.New(cwd, agentDir)
		}
		if f.name != "" && sess != nil {
			sess.SetName(f.name)
		}
	}

	exts := f.extension
	if f.noExtensions {
		exts = nil
	}

	eng, err := runtime.New(cmd.Context(), runtime.Options{
		Config:         cfg,
		Cwd:            cwd,
		AgentDir:       agentDir,
		Session:        sess,
		SystemPrompt:   f.systemPrompt,
		AppendSystem:   f.appendSystem,
		NoContextFiles: f.noContextFiles,
		NoSkills:       f.noSkills,
		SkillPaths:     f.skills,
		NoTools:        f.noTools,
		ToolAllow:      splitCSV(f.tools),
		ToolDeny:       splitCSV(f.excludeTools),
		Extensions:     exts,
		ContextWindow:  cfg.ContextWindow,
		NoPromptTpls:   f.noPromptTpls,
		PromptPaths:    f.promptTemplates,
		Models:         splitCSV(f.modelsFlag),
	})
	if err != nil {
		return err
	}
	defer eng.Close()

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
		return tui.RunEngine(cfg, eng)
	case "text", "print":
		if prompt == "" {
			fmt.Fprintf(out, "provider=%s model=%s theme=%s\n", cfg.ResolvedProvider(), cfg.ResolvedModel(), cfg.Theme)
			return nil
		}
		return eng.PrintText(ctx, out, history, prompt)
	case "json":
		if prompt == "" {
			return fmt.Errorf("--mode json requires a prompt")
		}
		return eng.PrintJSON(ctx, out, history, prompt)
	case "rpc":
		return eng.ServeRPC(ctx, cmd.InOrStdin(), out)
	default:
		return fmt.Errorf("unknown --mode %q (want: interactive|text|json|rpc)", mode)
	}
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
			provider, _ := cmd.Flags().GetString("provider")
			model, _ := cmd.Flags().GetString("model")
			if provider == "" && model != "" {
				p, _, _ := models.ParseSpec(model)
				provider = p
			}
			if provider == "" {
				return fmt.Errorf("Credential printing requires --provider <provider> or --model <model>")
			}
			key := auth.APIKey(config.DefaultConfigDir(), provider)
			if key == "" {
				return fmt.Errorf("no API key stored for %s", provider)
			}
			fmt.Fprintln(cmd.OutOrStdout(), key)
			return nil
		},
	}
	printKey.Flags().String("provider", "", "provider id")
	printKey.Flags().String("model", "", "model spec (provider inferred)")
	printBearer := &cobra.Command{
		Use:   "print-bearer-token",
		Short: "print an OAuth bearer token (not implemented)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("OAuth bearer tokens are not implemented")
		},
	}
	check := &cobra.Command{
		Use:   "check",
		Short: "check whether a provider has an API key (OAuth refresh is not implemented)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			provider, _ := cmd.Flags().GetString("provider")
			model, _ := cmd.Flags().GetString("model")
			asJSON, _ := cmd.Flags().GetBool("json")
			showCreds, _ := cmd.Flags().GetBool("credentials")
			if provider == "" && model != "" {
				p, _, _ := models.ParseSpec(model)
				provider = p
			}
			if provider == "" {
				return fmt.Errorf("Auth checks require --provider <provider> or --model <model>")
			}
			key := auth.APIKey(config.DefaultConfigDir(), provider)
			ok := key != ""
			if asJSON {
				payload := map[string]any{"provider": provider, "ok": ok, "type": "api_key"}
				if showCreds && ok {
					payload["credentials"] = key
				}
				b, _ := json.Marshal(payload)
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
				if !ok {
					return fmt.Errorf("no API key for %s", provider)
				}
				return nil
			}
			if !ok {
				return fmt.Errorf("no API key for %s", provider)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: ok (api_key)\n", provider)
			if showCreds {
				fmt.Fprintln(cmd.OutOrStdout(), key)
			}
			return nil
		},
	}
	check.Flags().String("provider", "", "provider id")
	check.Flags().String("model", "", "model spec")
	check.Flags().Bool("json", false, "JSON output")
	check.Flags().Bool("credentials", false, "include the credential")
	check.Flags().Bool("no-refresh", false, "ignored (OAuth refresh is not implemented)")
	cmd.AddCommand(login, logout, printKey, printBearer, check)
	return cmd
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "show or write settings.json"}
	cmd.Flags().Bool("print", false, "print resolved settings")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		dir := config.DefaultConfigDir()
		cfg, err := config.Load(dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "dir=%s\nprovider=%s\nmodel=%s\ntheme=%s\nthinking=%s\n",
			dir, cfg.ResolvedProvider(), cfg.ResolvedModel(), cfg.Theme, cfg.Thinking)
		return nil
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
