package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds the resolved pigo settings. Keys are defaultProvider /
// defaultModel / theme, with aliases (provider / model) for the earlier scaffold.
type Config struct {
	Provider               string                `mapstructure:"provider"`
	Model                  string                `mapstructure:"model"`
	DefaultProvider        string                `mapstructure:"defaultProvider"`
	DefaultModel           string                `mapstructure:"defaultModel"`
	Theme                  string                `mapstructure:"theme"`
	Thinking               string                `mapstructure:"thinking"`
	DefaultThinkingLevel   string                `mapstructure:"defaultThinkingLevel"`
	ContextWindow          int                   `mapstructure:"contextWindow"`
	CompactionOn           *bool                 `mapstructure:"compactionEnabled"`
	ReserveTokens          int                   `mapstructure:"compactionReserveTokens"`
	KeepRecentTokens       int                   `mapstructure:"compactionKeepRecentTokens"`
	Compaction             CompactionSettings    `mapstructure:"compaction"`
	SteeringMode           string                `mapstructure:"steeringMode"`
	FollowUpMode           string                `mapstructure:"followUpMode"`
	Retry                  RetrySettings         `mapstructure:"retry"`
	ThinkingBudgets        map[string]int        `mapstructure:"thinkingBudgets"`
	ModelThinkingLevels    map[string]string     `mapstructure:"modelThinkingLevels"`
	HTTPIdleTimeoutMs      *int                  `mapstructure:"httpIdleTimeoutMs"`
	ExternalEditor         string                `mapstructure:"externalEditor"`
	DoubleEscapeAction     string                `mapstructure:"doubleEscapeAction"`
	TreeFilterMode         string                `mapstructure:"treeFilterMode"`
	BranchSummary          BranchSummarySettings `mapstructure:"branchSummary"`
	Terminal               TerminalSettings      `mapstructure:"terminal"`
	Markdown               MarkdownSettings      `mapstructure:"markdown"`
	Images                 ImageSettings         `mapstructure:"images"`
	TUIMode                string                `mapstructure:"tuiMode"`
	FullscreenExitOutput   string                `mapstructure:"fullscreenExitOutput"`
	LastChangelogVersion   string                `mapstructure:"lastChangelogVersion"`
	CollapseChangelog      *bool                 `mapstructure:"collapseChangelog"`
	EnableInstallTelemetry *bool                 `mapstructure:"enableInstallTelemetry"`
	DefaultTools           *[]string             `mapstructure:"defaultTools"`
	EnabledModels          []string              `mapstructure:"enabledModels"`
	DefaultProjectTrust    string                `mapstructure:"defaultProjectTrust"`
	SessionDir             string                `mapstructure:"sessionDir"`
	QuietStartupFlag       *bool                 `mapstructure:"quietStartup"`
	HTTPProxy              string                `mapstructure:"httpProxy"`
	EnableSkillCommands    *bool                 `mapstructure:"enableSkillCommands"`

	Packages   []PackageEntry `mapstructure:"-" json:"packages,omitempty"`
	Extensions []string       `mapstructure:"-" json:"extensions,omitempty"`
	Skills     []string       `mapstructure:"-" json:"skills,omitempty"`
	Prompts    []string       `mapstructure:"-" json:"prompts,omitempty"`
	Themes     []string       `mapstructure:"-" json:"themes,omitempty"`
	NpmCommand []string       `mapstructure:"-" json:"npmCommand,omitempty"`
}

// CompactionSettings is pi settings.compaction.
type CompactionSettings struct {
	Enabled          *bool `mapstructure:"enabled" json:"enabled,omitempty"`
	ReserveTokens    int   `mapstructure:"reserveTokens" json:"reserveTokens,omitempty"`
	KeepRecentTokens int   `mapstructure:"keepRecentTokens" json:"keepRecentTokens,omitempty"`
}

// RetrySettings is pi settings.retry (enabled default true, maxRetries 3, baseDelayMs 2000).
type RetrySettings struct {
	Enabled     *bool `mapstructure:"enabled" json:"enabled,omitempty"`
	MaxRetries  *int  `mapstructure:"maxRetries" json:"maxRetries,omitempty"`
	BaseDelayMs *int  `mapstructure:"baseDelayMs" json:"baseDelayMs,omitempty"`
}

// BranchSummarySettings is pi settings.branchSummary.
type BranchSummarySettings struct {
	SkipPrompt    *bool `mapstructure:"skipPrompt" json:"skipPrompt,omitempty"`
	ReserveTokens int   `mapstructure:"reserveTokens" json:"reserveTokens,omitempty"`
}

// TerminalSettings is settings.terminal.
type TerminalSettings struct {
	ShowImages *bool `mapstructure:"showImages" json:"showImages,omitempty"`
}

// ImageSettings is settings.images.
type ImageSettings struct {
	BlockImages *bool `mapstructure:"blockImages" json:"blockImages,omitempty"`
}

// MarkdownSettings is settings.markdown.
type MarkdownSettings struct {
	Mermaid string `mapstructure:"mermaid" json:"mermaid,omitempty"`
}

// ResourceKinds is the settings.json key order for discovered resources.
var ResourceKinds = []string{"extensions", "skills", "prompts", "themes"}

// ResourcePaths returns the top-level path/override list for a resource kind.
func (c *Config) ResourcePaths(kind string) []string {
	switch kind {
	case "extensions":
		return c.Extensions
	case "skills":
		return c.Skills
	case "prompts":
		return c.Prompts
	case "themes":
		return c.Themes
	default:
		return nil
	}
}

// SetResourcePaths writes the top-level path/override list for a resource kind.
func (c *Config) SetResourcePaths(kind string, paths []string) {
	switch kind {
	case "extensions":
		c.Extensions = paths
	case "skills":
		c.Skills = paths
	case "prompts":
		c.Prompts = paths
	case "themes":
		c.Themes = paths
	}
}

// CompactionEnabled reports whether auto-compaction is on (default true).
func (c Config) CompactionEnabled() bool {
	if c.CompactionOn == nil {
		return true
	}
	return *c.CompactionOn
}

func applyNestedCompaction(cfg *Config) {
	if cfg.Compaction.Enabled != nil {
		cfg.CompactionOn = cfg.Compaction.Enabled
	}
	if cfg.Compaction.ReserveTokens > 0 {
		cfg.ReserveTokens = cfg.Compaction.ReserveTokens
	}
	if cfg.Compaction.KeepRecentTokens > 0 {
		cfg.KeepRecentTokens = cfg.Compaction.KeepRecentTokens
	}
}

// QuietStartup reports whether verbose startup listings are suppressed (default false).
func (c Config) QuietStartup() bool {
	return c.QuietStartupFlag != nil && *c.QuietStartupFlag
}

// SkillCommandsEnabled reports whether skills register as /skill:name (default true).
func (c Config) SkillCommandsEnabled() bool {
	if c.EnableSkillCommands == nil {
		return true
	}
	return *c.EnableSkillCommands
}

// CollapsedChangelog reports whether startup changelog is condensed (default false).
func (c Config) CollapsedChangelog() bool {
	return c.CollapseChangelog != nil && *c.CollapseChangelog
}

// InstallTelemetryEnabled reports whether changelog-detected updates ping pi.dev (default true).
func (c Config) InstallTelemetryEnabled() bool {
	if c.EnableInstallTelemetry == nil {
		return true
	}
	return *c.EnableInstallTelemetry
}

// RetryEnabled reports whether auto-retry is on (default true, like pi).
func (c Config) RetryEnabled() bool {
	if c.Retry.Enabled == nil {
		return true
	}
	return *c.Retry.Enabled
}

// RetryMaxRetries is the retry budget (default 3). 0 means no retries.
func (c Config) RetryMaxRetries() int {
	if c.Retry.MaxRetries == nil {
		return 3
	}
	return *c.Retry.MaxRetries
}

// RetryBaseDelayMs is the exponential backoff base (default 2000).
func (c Config) RetryBaseDelayMs() int {
	if c.Retry.BaseDelayMs == nil {
		return 2000
	}
	return *c.Retry.BaseDelayMs
}

// HTTPIdleTimeout is the HTTP client timeout (default 5m). 0 disables it.
func (c Config) HTTPIdleTimeout() time.Duration {
	if c.HTTPIdleTimeoutMs == nil {
		return 5 * time.Minute
	}
	if *c.HTTPIdleTimeoutMs <= 0 {
		return 0
	}
	return time.Duration(*c.HTTPIdleTimeoutMs) * time.Millisecond
}

// DoubleEscape is tree, fork, or none (default tree).
func (c Config) DoubleEscape() string {
	switch c.DoubleEscapeAction {
	case "fork", "none", "tree":
		return c.DoubleEscapeAction
	default:
		return "tree"
	}
}

// DefaultBuiltinTools is the initial built-in selection when defaultTools is unset.
func DefaultBuiltinTools() []string {
	return []string{"read", "bash", "edit", "write"}
}

// InitialBuiltinTools is settings.defaultTools when set (including an empty list),
// otherwise read/bash/edit/write.
func (c Config) InitialBuiltinTools() []string {
	if c.DefaultTools != nil {
		return append([]string(nil), (*c.DefaultTools)...)
	}
	return DefaultBuiltinTools()
}

// ShowImages reports whether the TUI should inline tool-result images (default true).
func (c Config) ShowImages() bool {
	if c.Terminal.ShowImages == nil {
		return true
	}
	return *c.Terminal.ShowImages
}

// BlockImages reports whether images should be omitted from LLM requests (default false).
func (c Config) BlockImages() bool {
	return c.Images.BlockImages != nil && *c.Images.BlockImages
}

// ProjectTrustDefault is ask, always, or never (default ask).
func (c Config) ProjectTrustDefault() string {
	switch strings.ToLower(strings.TrimSpace(c.DefaultProjectTrust)) {
	case "always", "never", "ask":
		return strings.ToLower(strings.TrimSpace(c.DefaultProjectTrust))
	default:
		return "ask"
	}
}

// MermaidMode is off, final, or streaming (default streaming).
func (c Config) MermaidMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Markdown.Mermaid)) {
	case "off", "final", "streaming":
		return strings.ToLower(strings.TrimSpace(c.Markdown.Mermaid))
	default:
		return "streaming"
	}
}

// TuiMode is regular or fullscreen (default regular).
func (c Config) TuiMode() string {
	if strings.EqualFold(strings.TrimSpace(c.TUIMode), "fullscreen") {
		return "fullscreen"
	}
	return "regular"
}

// FullscreenExit is transcript or resume-hint (default transcript).
func (c Config) FullscreenExit() string {
	if strings.EqualFold(strings.TrimSpace(c.FullscreenExitOutput), "resume-hint") {
		return "resume-hint"
	}
	return "transcript"
}

// TreeFilter is the initial /tree filter (default "default").
func (c Config) TreeFilter() string {
	switch c.TreeFilterMode {
	case "no-tools", "user-only", "labeled-only", "all", "default":
		return c.TreeFilterMode
	default:
		return "default"
	}
}

// BranchSummarySkipPrompt reports whether to skip the summarize dialog.
func (c Config) BranchSummarySkipPrompt() bool {
	if c.BranchSummary.SkipPrompt == nil {
		return false
	}
	return *c.BranchSummary.SkipPrompt
}

// BranchSummaryReserveTokens is the summarization token reserve (default 16384).
func (c Config) BranchSummaryReserveTokens() int {
	if c.BranchSummary.ReserveTokens > 0 {
		return c.BranchSummary.ReserveTokens
	}
	if c.ReserveTokens > 0 {
		return c.ReserveTokens
	}
	return 16384
}

// ModelThinkingLevel is the per-model default thinking override.
func (c Config) ModelThinkingLevel(provider, id string) string {
	if len(c.ModelThinkingLevels) == 0 || provider == "" || id == "" {
		return ""
	}
	return c.ModelThinkingLevels[provider+"/"+id]
}

// ResolvedProvider is the session provider, falling back to the saved default.
func (c Config) ResolvedProvider() string {
	if c.Provider != "" {
		return c.Provider
	}
	return c.DefaultProvider
}

// ResolvedModel is the session model, falling back to the saved default.
func (c Config) ResolvedModel() string {
	if c.Model != "" {
		return c.Model
	}
	return c.DefaultModel
}

// ExternalEditorCommand is settings.externalEditor, then $VISUAL, $EDITOR, then nano/notepad.
func (c Config) ExternalEditorCommand() string {
	if s := strings.TrimSpace(c.ExternalEditor); s != "" {
		return s
	}
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "nano"
}

// DefaultConfigDir is ~/.pigo/agent (override with PIGO_CODING_AGENT_DIR).
func DefaultConfigDir() string {
	if d := os.Getenv("PIGO_CODING_AGENT_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".pigo", "agent")
	}
	return filepath.Join(home, ".pigo", "agent")
}

// Load reads settings.json from configDir. A missing file is not an error.
func Load(configDir string) (Config, error) {
	v := viper.New()
	v.SetConfigName("settings")
	v.SetConfigType("json")
	v.AddConfigPath(configDir)

	v.SetDefault("provider", "anthropic")
	v.SetDefault("model", "claude-sonnet-4")
	v.SetDefault("theme", "default")
	v.SetDefault("thinking", "off")
	v.SetDefault("contextWindow", 200000)
	v.SetDefault("compactionReserveTokens", 16384)
	v.SetDefault("compactionKeepRecentTokens", 20000)
	v.SetDefault("steeringMode", "one-at-a-time")
	v.SetDefault("followUpMode", "one-at-a-time")
	v.SetDefault("doubleEscapeAction", "tree")
	v.SetDefault("treeFilterMode", "default")

	v.SetEnvPrefix("PIGO")
	v.AutomaticEnv()

	var cfg Config
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return cfg, err
		}
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, err
	}
	if cfg.Theme == "" {
		cfg.Theme = "default"
	}
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = 200000
	}
	if cfg.Thinking == "" && cfg.DefaultThinkingLevel != "" {
		cfg.Thinking = cfg.DefaultThinkingLevel
	}
	applyThinkingAlias(configDir, &cfg)
	applyNestedCompaction(&cfg)
	fillPackagesFromFile(configDir, &cfg)
	// Session current starts at the saved default (settings.json uses default*).
	if cfg.DefaultProvider != "" {
		cfg.Provider = cfg.DefaultProvider
	}
	if cfg.DefaultModel != "" {
		cfg.Model = cfg.DefaultModel
	}
	return cfg, nil
}

func applyThinkingAlias(configDir string, cfg *Config) {
	if cfg.DefaultThinkingLevel == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err != nil {
		return
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		return
	}
	if _, ok := raw["thinking"]; ok {
		return
	}
	cfg.Thinking = cfg.DefaultThinkingLevel
}

func fillPackagesFromFile(configDir string, cfg *Config) {
	b, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err != nil {
		return
	}
	var extra struct {
		Packages     []PackageEntry  `json:"packages"`
		Extensions   []string        `json:"extensions"`
		Skills       []string        `json:"skills"`
		Prompts      []string        `json:"prompts"`
		Themes       []string        `json:"themes"`
		NpmCommand   []string        `json:"npmCommand"`
		DefaultTools json.RawMessage `json:"defaultTools"`
	}
	if err := json.Unmarshal(b, &extra); err != nil {
		return
	}
	cfg.Packages = extra.Packages
	cfg.Extensions = extra.Extensions
	cfg.Skills = extra.Skills
	cfg.Prompts = extra.Prompts
	cfg.Themes = extra.Themes
	cfg.NpmCommand = extra.NpmCommand
	if extra.DefaultTools != nil {
		var tools []string
		if json.Unmarshal(extra.DefaultTools, &tools) == nil {
			cfg.DefaultTools = &tools
		}
	}
}

// Save writes settings.json, merging with any existing file so extra keys
// (and packages/extensions) are not dropped.
func Save(configDir string, cfg Config) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(configDir, "settings.json")
	existing := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &existing)
	}
	mergeSaveMap(existing, cfg)
	b, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func mergeSaveMap(existing map[string]any, cfg Config) {
	dp, dm := cfg.DefaultProvider, cfg.DefaultModel
	if dp == "" {
		dp = cfg.Provider
	}
	if dm == "" {
		dm = cfg.Model
	}
	existing["defaultProvider"] = dp
	existing["defaultModel"] = dm
	existing["theme"] = cfg.Theme
	existing["thinking"] = cfg.Thinking
	existing["contextWindow"] = cfg.ContextWindow
	existing["compaction"] = map[string]any{
		"enabled":          cfg.CompactionEnabled(),
		"reserveTokens":    cfg.ReserveTokens,
		"keepRecentTokens": cfg.KeepRecentTokens,
	}
	existing["compactionEnabled"] = cfg.CompactionEnabled()
	existing["compactionReserveTokens"] = cfg.ReserveTokens
	existing["compactionKeepRecentTokens"] = cfg.KeepRecentTokens
	existing["steeringMode"] = cfg.SteeringMode
	existing["followUpMode"] = cfg.FollowUpMode
	existing["lastChangelogVersion"] = cfg.LastChangelogVersion
	existing["collapseChangelog"] = cfg.CollapsedChangelog()
	existing["enableInstallTelemetry"] = cfg.InstallTelemetryEnabled()
	existing["doubleEscapeAction"] = cfg.DoubleEscape()
	existing["treeFilterMode"] = cfg.TreeFilter()
	existing["tuiMode"] = cfg.TuiMode()
	existing["fullscreenExitOutput"] = cfg.FullscreenExit()
	existing["defaultProjectTrust"] = cfg.ProjectTrustDefault()
	existing["retry"] = map[string]any{
		"enabled":     cfg.RetryEnabled(),
		"maxRetries":  cfg.RetryMaxRetries(),
		"baseDelayMs": cfg.RetryBaseDelayMs(),
	}
	if cfg.Packages != nil {
		existing["packages"] = cfg.Packages
	}
	if cfg.Extensions != nil {
		existing["extensions"] = cfg.Extensions
	}
	if cfg.Skills != nil {
		existing["skills"] = cfg.Skills
	}
	if cfg.Prompts != nil {
		existing["prompts"] = cfg.Prompts
	}
	if cfg.Themes != nil {
		existing["themes"] = cfg.Themes
	}
	if cfg.NpmCommand != nil {
		existing["npmCommand"] = cfg.NpmCommand
	}
	if cfg.DefaultTools != nil {
		existing["defaultTools"] = *cfg.DefaultTools
	}
	if cfg.EnabledModels != nil {
		existing["enabledModels"] = cfg.EnabledModels
	}
	if cfg.SessionDir != "" {
		existing["sessionDir"] = cfg.SessionDir
	}
	if cfg.QuietStartupFlag != nil {
		existing["quietStartup"] = *cfg.QuietStartupFlag
	}
	if cfg.HTTPProxy != "" {
		existing["httpProxy"] = cfg.HTTPProxy
	}
	if cfg.EnableSkillCommands != nil {
		existing["enableSkillCommands"] = *cfg.EnableSkillCommands
	}
	if cfg.DefaultThinkingLevel != "" {
		existing["defaultThinkingLevel"] = cfg.DefaultThinkingLevel
	}
	if cfg.ExternalEditor != "" {
		existing["externalEditor"] = cfg.ExternalEditor
	}
	if cfg.Terminal.ShowImages != nil {
		term, _ := existing["terminal"].(map[string]any)
		if term == nil {
			term = map[string]any{}
		}
		term["showImages"] = *cfg.Terminal.ShowImages
		existing["terminal"] = term
	}
	if cfg.Images.BlockImages != nil {
		images, _ := existing["images"].(map[string]any)
		if images == nil {
			images = map[string]any{}
		}
		images["blockImages"] = *cfg.Images.BlockImages
		existing["images"] = images
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Markdown.Mermaid)) {
	case "off", "final", "streaming":
		md, _ := existing["markdown"].(map[string]any)
		if md == nil {
			md = map[string]any{}
		}
		md["mermaid"] = strings.ToLower(strings.TrimSpace(cfg.Markdown.Mermaid))
		existing["markdown"] = md
	}
}
