package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Lowpower/pigo/internal/llama"
	"github.com/Lowpower/pigo/internal/models"
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
	registerProvider(Provider{
		ID: llama.ProviderID,
		APIKey: &APIKeyHandler{
			Name:  "llama.cpp server",
			Env:   []string{"LLAMA_API_KEY"},
			Login: llamaLogin,
		},
	})
	registerCatalogAPIKeys()
	registerCloudflareAuth()
	registerAzureAuth()
	registerVertexAuth()
	registerRadiusAuth()
}

func registerProvider(p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.ID] = p
}

// RegisterProvider adds or replaces an auth provider (extension overlay).
func RegisterProvider(p Provider) {
	registerProvider(p)
}

// UnregisterProvider removes a dynamically registered auth provider.
func UnregisterProvider(id string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, id)
}

func registerCatalogAPIKeys() {
	for _, id := range models.ProviderIDs() {
		spec, ok := models.LookupProvider(id)
		if !ok || len(spec.Env) == 0 {
			continue
		}
		name := spec.Name
		if name == "" {
			name = id + " API key"
		}
		existing, found := Lookup(id)
		if found {
			if existing.APIKey == nil {
				existing.APIKey = &APIKeyHandler{Name: name, Env: spec.Env, Login: promptAPIKey(name)}
				registerProvider(existing)
			}
			continue
		}
		registerProvider(Provider{
			ID: id,
			APIKey: &APIKeyHandler{
				Name:  name,
				Env:   spec.Env,
				Login: promptAPIKey(name),
			},
		})
	}
}

func registerCloudflareAuth() {
	registerProvider(Provider{
		ID: "cloudflare-workers-ai",
		APIKey: &APIKeyHandler{
			Name:    "Cloudflare API key",
			Login:   cloudflareLogin(false),
			Resolve: resolveCloudflare(false),
		},
	})
	registerProvider(Provider{
		ID: "cloudflare-ai-gateway",
		APIKey: &APIKeyHandler{
			Name:    "Cloudflare API key",
			Login:   cloudflareLogin(true),
			Resolve: resolveCloudflare(true),
		},
	})
}

func cloudflareLogin(gateway bool) func(Interaction) (Credential, error) {
	return func(ix Interaction) (Credential, error) {
		if ix.Prompt == nil {
			return Credential{}, fmt.Errorf("no prompt available")
		}
		key, err := ix.Prompt(Prompt{Type: PromptSecret, Message: "Cloudflare API key:"})
		if err != nil {
			return Credential{}, err
		}
		if key == "" {
			return Credential{}, fmt.Errorf("empty API key")
		}
		account, err := ix.Prompt(Prompt{Type: PromptText, Message: "Cloudflare account ID:"})
		if err != nil {
			return Credential{}, err
		}
		if account == "" {
			return Credential{}, fmt.Errorf("empty account ID")
		}
		env := map[string]string{"CLOUDFLARE_ACCOUNT_ID": account}
		if gateway {
			gw, err := ix.Prompt(Prompt{Type: PromptText, Message: "Cloudflare AI Gateway ID:"})
			if err != nil {
				return Credential{}, err
			}
			if gw == "" {
				return Credential{}, fmt.Errorf("empty gateway ID")
			}
			env["CLOUDFLARE_GATEWAY_ID"] = gw
		}
		return Credential{Type: TypeAPIKey, Key: key, Env: env}, nil
	}
}

func resolveCloudflare(gateway bool) func() *Result {
	return func() *Result {
		key := os.Getenv("CLOUDFLARE_API_KEY")
		account := os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		if key == "" || account == "" {
			return nil
		}
		env := map[string]string{"CLOUDFLARE_ACCOUNT_ID": account}
		auth := ModelAuth{APIKey: key}
		if gateway {
			gw := os.Getenv("CLOUDFLARE_GATEWAY_ID")
			if gw == "" {
				return nil
			}
			env["CLOUDFLARE_GATEWAY_ID"] = gw
			auth.APIKey = ""
			auth.Headers = map[string]string{"cf-aig-authorization": "Bearer " + key}
		}
		return &Result{Auth: auth, Env: env, Source: "CLOUDFLARE_API_KEY"}
	}
}

func registerAzureAuth() {
	registerProvider(Provider{
		ID: "azure-openai-responses",
		APIKey: &APIKeyHandler{
			Name:    "Azure OpenAI API key",
			Login:   azureLogin,
			Resolve: resolveAzure,
		},
	})
}

func azureLogin(ix Interaction) (Credential, error) {
	if ix.Prompt == nil {
		return Credential{}, fmt.Errorf("no prompt available")
	}
	key, err := ix.Prompt(Prompt{Type: PromptSecret, Message: "Azure OpenAI API key:"})
	if err != nil {
		return Credential{}, err
	}
	if key == "" {
		return Credential{}, fmt.Errorf("empty API key")
	}
	endpoint, err := ix.Prompt(Prompt{Type: PromptText, Message: "Azure OpenAI base URL or resource name:"})
	if err != nil {
		return Credential{}, err
	}
	if endpoint == "" {
		return Credential{}, fmt.Errorf("empty Azure endpoint")
	}
	env := map[string]string{}
	if strings.Contains(endpoint, "://") {
		env["AZURE_OPENAI_BASE_URL"] = endpoint
	} else {
		env["AZURE_OPENAI_RESOURCE_NAME"] = endpoint
	}
	return Credential{Type: TypeAPIKey, Key: key, Env: env}, nil
}

func resolveAzure() *Result {
	key := os.Getenv("AZURE_OPENAI_API_KEY")
	base := azureAuthBaseURL()
	if key == "" || base == "" {
		return nil
	}
	return &Result{Auth: ModelAuth{APIKey: key, BaseURL: base}, Source: "AZURE_OPENAI_API_KEY"}
}

func azureAuthBaseURL() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("AZURE_OPENAI_BASE_URL")), "/"); v != "" {
		return v
	}
	if res := strings.TrimSpace(os.Getenv("AZURE_OPENAI_RESOURCE_NAME")); res != "" {
		return "https://" + res + ".openai.azure.com/openai/v1"
	}
	return ""
}

func registerVertexAuth() {
	registerProvider(Provider{
		ID: "google-vertex",
		APIKey: &APIKeyHandler{
			Name:    "Google Cloud credentials",
			Login:   vertexLogin,
			Resolve: resolveVertex,
		},
	})
}

func vertexLogin(ix Interaction) (Credential, error) {
	if ix.Prompt == nil {
		return Credential{}, fmt.Errorf("no prompt available")
	}
	method, err := ix.Prompt(Prompt{
		Type:    PromptSelect,
		Message: "Select Google Vertex AI authentication method:",
		Options: []SelectOption{
			{ID: "api-key", Label: "Google Cloud API key"},
			{ID: "adc", Label: "Application Default Credentials"},
			{ID: "service-account", Label: "Service account credentials file"},
		},
	})
	if err != nil {
		return Credential{}, err
	}
	if method == "" {
		method = "api-key"
	}
	if method == "api-key" {
		key, err := ix.Prompt(Prompt{Type: PromptSecret, Message: "Enter Google Cloud API key"})
		if err != nil {
			return Credential{}, err
		}
		if key == "" {
			return Credential{}, fmt.Errorf("empty API key")
		}
		return Credential{Type: TypeAPIKey, Key: key}, nil
	}
	project, err := ix.Prompt(Prompt{Type: PromptText, Message: "Enter Google Cloud project ID"})
	if err != nil {
		return Credential{}, err
	}
	if project == "" {
		return Credential{}, fmt.Errorf("empty project ID")
	}
	location, err := ix.Prompt(Prompt{Type: PromptText, Message: "Enter Google Cloud location"})
	if err != nil {
		return Credential{}, err
	}
	if location == "" {
		return Credential{}, fmt.Errorf("empty location")
	}
	env := map[string]string{
		"GOOGLE_CLOUD_PROJECT":  project,
		"GOOGLE_CLOUD_LOCATION": location,
	}
	if method == "service-account" {
		path, err := ix.Prompt(Prompt{Type: PromptText, Message: "Enter service account credentials file path"})
		if err != nil {
			return Credential{}, err
		}
		if path == "" {
			return Credential{}, fmt.Errorf("empty credentials path")
		}
		env["GOOGLE_APPLICATION_CREDENTIALS"] = path
	}
	return Credential{Type: TypeAPIKey, Env: env}, nil
}

func resolveVertex() *Result {
	if key := os.Getenv("GOOGLE_CLOUD_API_KEY"); key != "" {
		return &Result{Auth: ModelAuth{APIKey: key}, Source: "GOOGLE_CLOUD_API_KEY"}
	}
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		project = os.Getenv("GCLOUD_PROJECT")
	}
	location := os.Getenv("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = os.Getenv("GOOGLE_CLOUD_REGION")
	}
	if project == "" || location == "" {
		return nil
	}
	adc := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if adc == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		adc = filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	}
	if _, err := os.Stat(adc); err != nil {
		return nil
	}
	return &Result{
		Auth:   ModelAuth{},
		Source: "gcloud application default credentials",
		Env: map[string]string{
			"GOOGLE_CLOUD_PROJECT":  project,
			"GOOGLE_CLOUD_LOCATION": location,
		},
	}
}

func registerRadiusAuth() {
	registerProvider(Provider{
		ID: "radius",
		APIKey: &APIKeyHandler{
			Name:  "Radius API key",
			Env:   []string{"RADIUS_API_KEY"},
			Login: promptAPIKey("Radius API key"),
		},
		OAuth: NewRadiusOAuth("Radius", "https://radius.pi.dev"),
	})
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
