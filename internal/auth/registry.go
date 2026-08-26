package auth

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	registryMu sync.Mutex
	registry   = map[string]Provider{}
)

func init() {
	registerBuiltins()
}

func registerBuiltins() {
	registerProvider(Provider{
		ID: "anthropic",
		APIKey: &APIKeyHandler{
			Name:  "Anthropic API key",
			Env:   []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
			Login: promptAPIKey("Anthropic API key"),
		},
		OAuth: anthropicOAuth{},
	})
	registerProvider(Provider{
		ID: "openai",
		APIKey: &APIKeyHandler{
			Name:  "OpenAI API key",
			Env:   []string{"OPENAI_API_KEY"},
			Login: promptAPIKey("OpenAI API key"),
		},
	})
	registerProvider(Provider{
		ID: "openai-codex",
		APIKey: &APIKeyHandler{
			Name:  "OpenAI API key",
			Env:   []string{"OPENAI_API_KEY"},
			Login: promptAPIKey("OpenAI API key"),
		},
		OAuth: openaiCodexOAuth{},
	})
	registerProvider(Provider{
		ID: "opencode",
		APIKey: &APIKeyHandler{
			Name:  "OpenCode API key",
			Env:   []string{"OPENCODE_API_KEY"},
			Login: promptAPIKey("OpenCode API key"),
		},
	})
	registerProvider(Provider{
		ID:    "github-copilot",
		OAuth: githubCopilotOAuth{},
	})
	registerProvider(Provider{
		ID: "openrouter",
		APIKey: &APIKeyHandler{
			Name:  "OpenRouter API key",
			Env:   []string{"OPENROUTER_API_KEY"},
			Login: promptAPIKey("OpenRouter API key"),
		},
		OAuth: openrouterOAuth{},
	})
	registerProvider(Provider{
		ID:    "kimi-coding",
		OAuth: kimiOAuth{},
	})
	registerProvider(Provider{
		ID: "xai",
		APIKey: &APIKeyHandler{
			Name:  "xAI API key",
			Env:   []string{"XAI_API_KEY", "GROK_API_KEY"},
			Login: promptAPIKey("xAI API key"),
		},
		OAuth: xaiOAuth{},
	})
	registerProvider(Provider{
		ID: "google",
		APIKey: &APIKeyHandler{
			Name:  "Gemini API key",
			Env:   []string{"GEMINI_API_KEY"},
			Login: promptAPIKey("Gemini API key"),
		},
	})
	registerProvider(Provider{
		ID: "amazon-bedrock",
		APIKey: &APIKeyHandler{
			Name:    "Amazon Bedrock",
			Env:     []string{"AWS_BEARER_TOKEN_BEDROCK"},
			Login:   promptAPIKey("Bedrock bearer token"),
			Resolve: bedrockAmbientAuth,
		},
	})
}

func registerProvider(p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.ID] = p
}

// Lookup returns a registered auth provider.
func Lookup(id string) (Provider, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	p, ok := registry[id]
	return p, ok
}

// Providers returns registered providers sorted by id.
func Providers() []Provider {
	registryMu.Lock()
	defer registryMu.Unlock()
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Provider, 0, len(ids))
	for _, id := range ids {
		out = append(out, registry[id])
	}
	return out
}

// AuthenticatedIDs returns registered providers that currently have credentials.
func AuthenticatedIDs(s *Store) []string {
	var out []string
	for _, p := range Providers() {
		if CheckAuth(s, p.ID) != nil {
			out = append(out, p.ID)
		}
	}
	return out
}

func promptAPIKey(label string) func(Interaction) (Credential, error) {
	return func(ix Interaction) (Credential, error) {
		if ix.Prompt == nil {
			return Credential{}, fmt.Errorf("no prompt available")
		}
		key, err := ix.Prompt(Prompt{Type: PromptSecret, Message: label + ":"})
		if err != nil {
			return Credential{}, err
		}
		if key == "" {
			return Credential{}, fmt.Errorf("empty API key")
		}
		return Credential{Type: TypeAPIKey, Key: key}, nil
	}
}

// Login runs oauth or api_key login and persists via modify.
func Login(ctx context.Context, s *Store, providerID, authType string, ix Interaction) error {
	p, ok := Lookup(providerID)
	if !ok {
		return fmt.Errorf("unknown provider %q", providerID)
	}
	if ix.Ctx == nil {
		ix.Ctx = ctx
	}
	var cred Credential
	var err error
	switch authType {
	case TypeOAuth:
		if p.OAuth == nil {
			return fmt.Errorf("provider %q has no OAuth login", providerID)
		}
		cred, err = p.OAuth.Login(ix)
	case TypeAPIKey:
		if p.APIKey == nil || p.APIKey.Login == nil {
			return fmt.Errorf("provider %q has no API key login", providerID)
		}
		cred, err = p.APIKey.Login(ix)
	default:
		return fmt.Errorf("unknown auth type %q", authType)
	}
	if err != nil {
		return err
	}
	_, err = s.Modify(providerID, func(*Credential) (*Credential, error) {
		return &cred, nil
	})
	return err
}

// CheckAuth is side-effect-free (no refresh).
func CheckAuth(s *Store, providerID string) *Check {
	p, ok := Lookup(providerID)
	if !ok {
		return nil
	}
	c, found, err := s.Read(providerID)
	if err != nil || !found {
		if p.APIKey != nil {
			if r, _ := resolveAPIKey(p, nil); r != nil {
				return &Check{Type: TypeAPIKey, Source: r.Source}
			}
		}
		return nil
	}
	return &Check{Type: c.Type}
}

// Secret extracts a printable API key or bearer token from resolved auth.
func Secret(r *Result) string {
	if r == nil {
		return ""
	}
	if r.Auth.APIKey != "" {
		return r.Auth.APIKey
	}
	for k, v := range r.Auth.Headers {
		if strings.EqualFold(k, "authorization") && len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
			return v[7:]
		}
	}
	return ""
}
