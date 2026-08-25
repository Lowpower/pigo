package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("provider: got %q, want %q", cfg.Provider, "anthropic")
	}
	if cfg.Model != "claude-sonnet-4" {
		t.Errorf("model: got %q, want %q", cfg.Model, "claude-sonnet-4")
	}
	if cfg.Theme != "default" {
		t.Errorf("theme: got %q, want %q", cfg.Theme, "default")
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("PIGO_PROVIDER", "openai")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Provider != "openai" {
		t.Errorf("provider: got %q, want %q", cfg.Provider, "openai")
	}
}

func TestLoadPiKeyNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultProvider":"openai","defaultModel":"gpt-4o","theme":"light"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResolvedProvider() != "openai" || cfg.ResolvedModel() != "gpt-4o" || cfg.Theme != "light" {
		t.Fatalf("got provider=%s model=%s theme=%s", cfg.ResolvedProvider(), cfg.ResolvedModel(), cfg.Theme)
	}
}
