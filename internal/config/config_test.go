package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	// An empty directory has no settings file, so defaults apply.
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
