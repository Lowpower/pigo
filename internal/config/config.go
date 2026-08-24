// Package config loads pigo settings from the config directory (default ~/.pi)
// using viper, with environment-variable overrides prefixed by PIGO_.
package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds the resolved pigo settings. It grows as later phases add fields.
type Config struct {
	Provider string `mapstructure:"provider"`
	Model    string `mapstructure:"model"`
	Theme    string `mapstructure:"theme"`
}

// DefaultConfigDir returns the default config directory (~/.pi), matching pi's
// storage location. It falls back to ".pi" in the working directory if the home
// directory cannot be determined.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".pi"
	}
	return filepath.Join(home, ".pi")
}

// Load reads settings.json from configDir. A missing file is not an error; the
// documented defaults are used instead. Environment variables (PIGO_PROVIDER,
// PIGO_MODEL, PIGO_THEME) override file and default values.
func Load(configDir string) (Config, error) {
	v := viper.New()
	v.SetConfigName("settings")
	v.SetConfigType("json")
	v.AddConfigPath(configDir)

	v.SetDefault("provider", "anthropic")
	v.SetDefault("model", "claude-sonnet-4")
	v.SetDefault("theme", "default")

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
	return cfg, nil
}
