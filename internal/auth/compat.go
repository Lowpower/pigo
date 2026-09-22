package auth

import (
	"os"

	"github.com/Lowpower/pigo/internal/secretname"
)

// SetAPIKey stores a provider API key.
func SetAPIKey(agentDir, provider, key string) error {
	_, err := Open(agentDir).Modify(provider, func(*Credential) (*Credential, error) {
		return &Credential{Type: TypeAPIKey, Key: key}, nil
	})
	return err
}

// Delete removes a provider's stored credentials.
func Delete(agentDir, provider string) error {
	return Open(agentDir).Delete(provider)
}

// Get returns a stored credential for provider (no template resolution beyond Read).
func Get(agentDir, provider string) (Credential, bool) {
	c, ok, err := Open(agentDir).Read(provider)
	if err != nil || !ok {
		return Credential{}, false
	}
	return c, true
}

// APIKey returns a stored API key, falling back to the usual env vars.
func APIKey(agentDir, provider string) string {
	if k := ProcessAPIKey(provider); k != "" {
		return k
	}
	if c, ok := Get(agentDir, provider); ok {
		if c.Type == TypeAPIKey && c.Key != "" {
			return c.Key
		}
	}
	return ambientAPIKey(provider)
}

func ambientAPIKey(provider string) string {
	switch provider {
	case "openai", "openai-codex":
		return os.Getenv("OPENAI_API_KEY")
	case "opencode":
		return os.Getenv("OPENCODE_API_KEY")
	case "openrouter":
		return os.Getenv("OPENROUTER_API_KEY")
	case "xai":
		if k := os.Getenv("XAI_API_KEY"); k != "" {
			return k
		}
		return os.Getenv("GROK_API_KEY")
	case "google":
		return os.Getenv("GEMINI_API_KEY")
	case "amazon-bedrock":
		return os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	default:
		if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
			return k
		}
		if k := os.Getenv("ANTHROPIC_OAUTH_TOKEN"); k != "" {
			return k
		}
		return os.Getenv("ANTHROPIC_AUTH_TOKEN")
	}
}

// ApplyEnv applies non-secret credential env overlays and Copilot catalog filters.
// API keys and OAuth tokens stay in auth.json; they are not copied into the process environment.
func ApplyEnv(agentDir string) {
	s := Open(agentDir)
	s.mu.Lock()
	data, err := s.loadUnlocked()
	s.mu.Unlock()
	if err != nil {
		return
	}
	set := func(env, key string) {
		if secretname.ShouldDrop(env) {
			return
		}
		if os.Getenv(env) == "" && key != "" {
			_ = os.Setenv(env, key)
		}
	}
	for id, c := range data {
		for env, val := range c.Env {
			set(env, val)
		}
		if id == "github-copilot" {
			applyCopilotAvailableModels(c)
		}
	}
}
