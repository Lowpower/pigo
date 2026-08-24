package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// File is the on-disk credential map (pi auth.json).
type File struct {
	Providers map[string]Credential `json:"providers"`
}

// Credential is an API key or OAuth placeholder.
type Credential struct {
	Type string `json:"type"` // api_key | oauth
	Key  string `json:"key,omitempty"`
}

func path(agentDir string) string { return filepath.Join(agentDir, "auth.json") }

// Load reads auth.json (missing file → empty).
func Load(agentDir string) (File, error) {
	b, err := os.ReadFile(path(agentDir))
	if err != nil {
		if os.IsNotExist(err) {
			return File{Providers: map[string]Credential{}}, nil
		}
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, err
	}
	if f.Providers == nil {
		f.Providers = map[string]Credential{}
	}
	return f, nil
}

// Save writes auth.json with 0600 permissions.
func Save(agentDir string, f File) error {
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(agentDir), append(b, '\n'), 0o600)
}

// SetAPIKey stores a provider API key.
func SetAPIKey(agentDir, provider, key string) error {
	f, err := Load(agentDir)
	if err != nil {
		return err
	}
	f.Providers[provider] = Credential{Type: "api_key", Key: key}
	return Save(agentDir, f)
}

// Delete removes a provider's stored credentials.
func Delete(agentDir, provider string) error {
	f, err := Load(agentDir)
	if err != nil {
		return err
	}
	delete(f.Providers, provider)
	return Save(agentDir, f)
}

// ApplyEnv sets provider env vars from auth.json when they are not already set.
func ApplyEnv(agentDir string) {
	f, err := Load(agentDir)
	if err != nil {
		return
	}
	set := func(env, key string) {
		if os.Getenv(env) == "" && key != "" {
			_ = os.Setenv(env, key)
		}
	}
	if c, ok := f.Providers["anthropic"]; ok {
		set("ANTHROPIC_API_KEY", c.Key)
	}
	if c, ok := f.Providers["openai"]; ok {
		set("OPENAI_API_KEY", c.Key)
	}
	if c, ok := f.Providers["opencode"]; ok {
		set("OPENCODE_API_KEY", c.Key)
	}
}
