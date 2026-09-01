package auth

import (
	"os"
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
		return os.Getenv("ANTHROPIC_AUTH_TOKEN")
	}
}

// ApplyEnv sets provider env vars from stored credentials when they are not already set.
func ApplyEnv(agentDir string) {
	s := Open(agentDir)
	s.mu.Lock()
	data, err := s.loadUnlocked()
	s.mu.Unlock()
	if err != nil {
		return
	}
	set := func(env, key string) {
		if os.Getenv(env) == "" && key != "" {
			_ = os.Setenv(env, key)
		}
	}
	for id, c := range data {
		key := c.Key
		if c.Type == TypeOAuth {
			key = c.Access
		} else if c.Type == TypeAPIKey && c.Key != "" {
			if resolved := ResolveConfigValue(c.Key, c.Env); resolved != "" {
				key = resolved
			}
		}
		for env, val := range c.Env {
			set(env, val)
		}
		switch id {
		case "anthropic":
			set("ANTHROPIC_API_KEY", key)
		case "openai", "openai-codex":
			set("OPENAI_API_KEY", key)
		case "opencode":
			set("OPENCODE_API_KEY", key)
		case "openrouter":
			set("OPENROUTER_API_KEY", key)
		case "google":
			set("GEMINI_API_KEY", key)
		case "google-vertex":
			set("GOOGLE_CLOUD_API_KEY", key)
		case "amazon-bedrock":
			set("AWS_BEARER_TOKEN_BEDROCK", key)
		case "radius":
			set("RADIUS_API_KEY", key)
		case "mistral":
			set("MISTRAL_API_KEY", key)
		}
		if id == "github-copilot" {
			applyCopilotAvailableModels(c)
		}
	}
}
