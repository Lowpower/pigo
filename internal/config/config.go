package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds the resolved pigo settings. Keys are defaultProvider /
// defaultModel / theme, with aliases (provider / model) for the earlier scaffold.
type Config struct {
	Provider         string `mapstructure:"provider"`
	Model            string `mapstructure:"model"`
	DefaultProvider  string `mapstructure:"defaultProvider"`
	DefaultModel     string `mapstructure:"defaultModel"`
	Theme            string `mapstructure:"theme"`
	Thinking         string `mapstructure:"thinking"`
	ContextWindow    int    `mapstructure:"contextWindow"`
	CompactionOn     *bool  `mapstructure:"compactionEnabled"`
	ReserveTokens    int    `mapstructure:"compactionReserveTokens"`
	KeepRecentTokens int    `mapstructure:"compactionKeepRecentTokens"`
	SteeringMode     string        `mapstructure:"steeringMode"`
	FollowUpMode     string        `mapstructure:"followUpMode"`
	Retry            RetrySettings `mapstructure:"retry"`
}

// RetrySettings is pi settings.retry (enabled default true, maxRetries 3, baseDelayMs 2000).
type RetrySettings struct {
	Enabled     *bool `mapstructure:"enabled" json:"enabled,omitempty"`
	MaxRetries  *int  `mapstructure:"maxRetries" json:"maxRetries,omitempty"`
	BaseDelayMs *int  `mapstructure:"baseDelayMs" json:"baseDelayMs,omitempty"`
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

// ResolvedProvider returns defaultProvider, falling back to provider.
func (c Config) ResolvedProvider() string {
	if c.DefaultProvider != "" {
		return c.DefaultProvider
	}
	return c.Provider
}

// ResolvedModel returns defaultModel, falling back to model.
func (c Config) ResolvedModel() string {
	if c.DefaultModel != "" {
		return c.DefaultModel
	}
	return c.Model
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
	return cfg, nil
}

// Save writes settings.json.
func Save(configDir string, cfg Config) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	payload := map[string]any{
		"defaultProvider":            cfg.ResolvedProvider(),
		"defaultModel":               cfg.ResolvedModel(),
		"theme":                      cfg.Theme,
		"thinking":                   cfg.Thinking,
		"contextWindow":              cfg.ContextWindow,
		"compactionEnabled":          cfg.CompactionEnabled(),
		"compactionReserveTokens":    cfg.ReserveTokens,
		"compactionKeepRecentTokens": cfg.KeepRecentTokens,
		"steeringMode":               cfg.SteeringMode,
		"followUpMode":               cfg.FollowUpMode,
		"retry": map[string]any{
			"enabled":     cfg.RetryEnabled(),
			"maxRetries":  cfg.RetryMaxRetries(),
			"baseDelayMs": cfg.RetryBaseDelayMs(),
		},
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, "settings.json"), append(b, '\n'), 0o644)
}
