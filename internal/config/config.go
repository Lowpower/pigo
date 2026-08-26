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
	Provider            string                `mapstructure:"provider"`
	Model               string                `mapstructure:"model"`
	DefaultProvider     string                `mapstructure:"defaultProvider"`
	DefaultModel        string                `mapstructure:"defaultModel"`
	Theme               string                `mapstructure:"theme"`
	Thinking            string                `mapstructure:"thinking"`
	ContextWindow       int                   `mapstructure:"contextWindow"`
	CompactionOn        *bool                 `mapstructure:"compactionEnabled"`
	ReserveTokens       int                   `mapstructure:"compactionReserveTokens"`
	KeepRecentTokens    int                   `mapstructure:"compactionKeepRecentTokens"`
	SteeringMode        string                `mapstructure:"steeringMode"`
	FollowUpMode        string                `mapstructure:"followUpMode"`
	Retry               RetrySettings         `mapstructure:"retry"`
	ThinkingBudgets     map[string]int        `mapstructure:"thinkingBudgets"`
	ModelThinkingLevels map[string]string     `mapstructure:"modelThinkingLevels"`
	HTTPIdleTimeoutMs   *int                  `mapstructure:"httpIdleTimeoutMs"`
	ExternalEditor      string                `mapstructure:"externalEditor"`
	DoubleEscapeAction  string                `mapstructure:"doubleEscapeAction"`
	TreeFilterMode      string                `mapstructure:"treeFilterMode"`
	BranchSummary       BranchSummarySettings `mapstructure:"branchSummary"`

	Packages   []PackageEntry `mapstructure:"-" json:"packages,omitempty"`
	Extensions []string       `mapstructure:"-" json:"extensions,omitempty"`
	Skills     []string       `mapstructure:"-" json:"skills,omitempty"`
	Prompts    []string       `mapstructure:"-" json:"prompts,omitempty"`
	Themes     []string       `mapstructure:"-" json:"themes,omitempty"`
	NpmCommand []string       `mapstructure:"-" json:"npmCommand,omitempty"`
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

func fillPackagesFromFile(configDir string, cfg *Config) {
	b, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err != nil {
		return
	}
	var extra struct {
		Packages   []PackageEntry `json:"packages"`
		Extensions []string       `json:"extensions"`
		Skills     []string       `json:"skills"`
		Prompts    []string       `json:"prompts"`
		Themes     []string       `json:"themes"`
		NpmCommand []string       `json:"npmCommand"`
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
	existing["compactionEnabled"] = cfg.CompactionEnabled()
	existing["compactionReserveTokens"] = cfg.ReserveTokens
	existing["compactionKeepRecentTokens"] = cfg.KeepRecentTokens
	existing["steeringMode"] = cfg.SteeringMode
	existing["followUpMode"] = cfg.FollowUpMode
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
	if cfg.ExternalEditor != "" {
		existing["externalEditor"] = cfg.ExternalEditor
	}
	b, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
