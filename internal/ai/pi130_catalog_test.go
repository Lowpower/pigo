package ai

import (
	"testing"

	"google.golang.org/genai"
)

func TestPi130OpenCodeGoGeminiAndMiniMaxAPI(t *testing.T) {
	if got := guessOpenCodeGoAPI("gemini-2.5-pro"); got != "google-generative-ai" {
		t.Fatalf("guess gemini = %q", got)
	}
	if got := resolveWireAPI("opencode-go", "gemini-2.5-pro", "openai-completions"); got != "google-generative-ai" {
		t.Fatalf("wire gemini = %q", got)
	}
	if got := resolveWireAPI("opencode-go", "minimax-m2.7", "openai-completions"); got != "anthropic-messages" {
		t.Fatalf("wire minimax-m2.7 = %q", got)
	}
	if got := resolveWireAPI("opencode-go", "minimax-m3", "openai-completions"); got != "openai-completions" {
		t.Fatalf("wire minimax-m3 = %q", got)
	}
	if got := guessOpenCodeGoAPI("gpt-5"); got != "openai-responses" {
		t.Fatalf("guess gpt-5 = %q", got)
	}
}

func TestPi130OpenCodeGoogleAPIVersion(t *testing.T) {
	c := &GoogleClient{APIKey: "k", BaseURL: "https://opencode.ai/inference"}
	cfg := c.clientConfig(Options{Provider: "opencode"})
	if cfg.HTTPOptions.APIVersion != "v1" || cfg.HTTPOptions.BaseURL != "https://opencode.ai/inference" {
		t.Fatalf("opencode google = %+v", cfg.HTTPOptions)
	}
	goCfg := c.clientConfig(Options{Provider: "opencode-go"})
	if goCfg.HTTPOptions.APIVersion != "v1" {
		t.Fatalf("opencode-go version = %q", goCfg.HTTPOptions.APIVersion)
	}
	plain := c.clientConfig(Options{Provider: "google"})
	if plain.HTTPOptions.APIVersion != "" || plain.Backend != genai.BackendGeminiAPI {
		t.Fatalf("direct google = %+v backend=%v", plain.HTTPOptions, plain.Backend)
	}
}
