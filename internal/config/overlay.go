package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// ApplyProject merges <cwd>/.pi/settings.json over user settings when trusted.
// Untrusted projects contribute an empty overlay.
func ApplyProject(base Config, cwd string, trusted bool) Config {
	if !trusted || strings.TrimSpace(cwd) == "" {
		return base
	}
	b, err := os.ReadFile(filepath.Join(cwd, ".pi", "settings.json"))
	if err != nil {
		return base
	}
	var overlay map[string]any
	if json.Unmarshal(b, &overlay) != nil || len(overlay) == 0 {
		return base
	}
	merged := deepMerge(settingsMap(base), overlay)
	cfg, err := decodeSettingsMap(merged)
	if err != nil {
		return base
	}
	cfg.Packages = base.Packages
	if overlayPkgs, ok := overlay["packages"]; ok {
		raw, _ := json.Marshal(overlayPkgs)
		var pkgs []PackageEntry
		if json.Unmarshal(raw, &pkgs) == nil {
			cfg.Packages = pkgs
		}
	}
	copyResourceSlices(&cfg, overlay, base)
	if cfg.Thinking == "" && cfg.DefaultThinkingLevel != "" {
		cfg.Thinking = cfg.DefaultThinkingLevel
	}
	applyNestedCompaction(&cfg)
	if cfg.DefaultProvider != "" {
		cfg.Provider = cfg.DefaultProvider
	}
	if cfg.DefaultModel != "" {
		cfg.Model = cfg.DefaultModel
	}
	return cfg
}

func copyResourceSlices(cfg *Config, overlay map[string]any, base Config) {
	cfg.Extensions = overlayStrings(overlay, "extensions", base.Extensions)
	cfg.Skills = overlayStrings(overlay, "skills", base.Skills)
	cfg.Prompts = overlayStrings(overlay, "prompts", base.Prompts)
	cfg.Themes = overlayStrings(overlay, "themes", base.Themes)
	cfg.NpmCommand = overlayStrings(overlay, "npmCommand", base.NpmCommand)
	cfg.EnabledModels = overlayStrings(overlay, "enabledModels", base.EnabledModels)
}

func overlayStrings(overlay map[string]any, key string, fallback []string) []string {
	v, ok := overlay[key]
	if !ok {
		return fallback
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fallback
	}
	var out []string
	if json.Unmarshal(raw, &out) != nil {
		return fallback
	}
	return out
}

func settingsMap(cfg Config) map[string]any {
	existing := map[string]any{}
	mergeSaveMap(existing, cfg)
	return existing
}

func decodeSettingsMap(m map[string]any) (Config, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return Config{}, err
	}
	v := viper.New()
	v.SetConfigType("json")
	if err := v.ReadConfig(bytes.NewReader(b)); err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func deepMerge(base, overlay map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		if v == nil {
			continue
		}
		bv, ok := out[k].(map[string]any)
		ov, ook := v.(map[string]any)
		if ok && ook {
			out[k] = deepMerge(bv, ov)
			continue
		}
		out[k] = v
	}
	return out
}
