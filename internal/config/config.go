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
	SteeringMode     string `mapstructure:"steeringMode"`
	FollowUpMode     string `mapstructure:"followUpMode"`

	Packages   []PackageEntry `mapstructure:"-" json:"packages,omitempty"`
	Extensions []string       `mapstructure:"-" json:"extensions,omitempty"`
	Skills     []string       `mapstructure:"-" json:"skills,omitempty"`
	Prompts    []string       `mapstructure:"-" json:"prompts,omitempty"`
	Themes     []string       `mapstructure:"-" json:"themes,omitempty"`
	NpmCommand []string       `mapstructure:"-" json:"npmCommand,omitempty"`
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
	fillPackagesFromFile(configDir, &cfg)
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
	existing["defaultProvider"] = cfg.ResolvedProvider()
	existing["defaultModel"] = cfg.ResolvedModel()
	existing["theme"] = cfg.Theme
	existing["thinking"] = cfg.Thinking
	existing["contextWindow"] = cfg.ContextWindow
	existing["compactionEnabled"] = cfg.CompactionEnabled()
	existing["compactionReserveTokens"] = cfg.ReserveTokens
	existing["compactionKeepRecentTokens"] = cfg.KeepRecentTokens
	existing["steeringMode"] = cfg.SteeringMode
	existing["followUpMode"] = cfg.FollowUpMode
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
	b, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
