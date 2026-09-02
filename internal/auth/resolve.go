package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/llama"
)

const (
	// DefaultOAuthMinValidityMs is the request-path refresh window (5 minutes).
	DefaultOAuthMinValidityMs = 5 * 60 * 1000
	oauthRefreshTimeout       = 15 * time.Second
)

// Resolve derives request auth for a provider (pi resolveProviderAuth).
func Resolve(ctx context.Context, s *Store, p Provider, opts ResolveOpts) (*Result, error) {
	if opts.APIKey != "" && p.APIKey != nil {
		return resolveAPIKey(p, &Credential{Type: TypeAPIKey, Key: opts.APIKey, Env: opts.Env})
	}
	cred, ok, err := s.Read(p.ID)
	if err != nil {
		return nil, fmt.Errorf("credential store read failed for %s: %w", p.ID, err)
	}
	if ok {
		if cred.Type == TypeOAuth && p.OAuth != nil {
			if opts.NoRefresh {
				auth, err := p.OAuth.ToAuth(cred)
				if err != nil {
					return nil, err
				}
				return &Result{Auth: auth, Source: "OAuth"}, nil
			}
			return resolveStoredOAuth(ctx, s, p, cred, opts.MinOAuthValidityMs)
		}
		if cred.Type == TypeAPIKey && p.APIKey != nil {
			if opts.Env != nil {
				if cred.Env == nil {
					cred.Env = map[string]string{}
				}
				for k, v := range opts.Env {
					cred.Env[k] = v
				}
			}
			return resolveAPIKey(p, &cred)
		}
		return nil, nil
	}
	if p.APIKey != nil {
		return resolveAPIKey(p, nil)
	}
	return nil, nil
}

func resolveStoredOAuth(ctx context.Context, s *Store, p Provider, stored Credential, minOAuthValidityMs int64) (*Result, error) {
	minimum := int64(DefaultOAuthMinValidityMs)
	if minOAuthValidityMs > minimum {
		minimum = minOAuthValidityMs
	}
	expiresSoon := func(c Credential) bool {
		return time.Now().UnixMilli()+minimum >= c.Expires
	}
	cred := stored
	if expiresSoon(cred) {
		post, err := s.Modify(p.ID, func(current *Credential) (*Credential, error) {
			if current == nil || current.Type != TypeOAuth {
				return nil, nil
			}
			if !expiresSoon(*current) {
				return nil, nil
			}
			rctx, cancel := context.WithTimeout(ctx, oauthRefreshTimeout)
			defer cancel()
			next, err := p.OAuth.Refresh(rctx, *current)
			if err != nil {
				return nil, fmt.Errorf("oAuth refresh failed for %s: %w", p.ID, err)
			}
			return &next, nil
		})
		if err != nil {
			return nil, err
		}
		if post == nil || post.Type != TypeOAuth {
			return nil, nil
		}
		cred = *post
		if minOAuthValidityMs != 0 && expiresSoon(cred) {
			return nil, fmt.Errorf("oAuth refresh returned a token that expires too soon for %s", p.ID)
		}
	}
	auth, err := p.OAuth.ToAuth(cred)
	if err != nil {
		return nil, fmt.Errorf("oAuth auth derivation failed for %s: %w", p.ID, err)
	}
	return &Result{Auth: auth, Source: "OAuth"}, nil
}

func resolveAPIKey(p Provider, cred *Credential) (*Result, error) {
	r, err := resolveAPIKeyInner(p, cred)
	if p.ID == llama.ProviderID {
		return llamaResult(cred, r), nil
	}
	return r, err
}

func resolveAPIKeyInner(p Provider, cred *Credential) (*Result, error) {
	var key string
	var env map[string]string
	if cred != nil {
		key = cred.Key
		env = cred.Env
	}
	if key == "" && p.APIKey != nil {
		for _, name := range p.APIKey.Env {
			if v := os.Getenv(name); v != "" {
				if name == "ANTHROPIC_AUTH_TOKEN" {
					return &Result{
						Auth:   ModelAuth{Headers: map[string]string{"Authorization": "Bearer " + v}},
						Source: name,
						Env:    env,
					}, nil
				}
				return &Result{Auth: ModelAuth{APIKey: v}, Source: name, Env: env}, nil
			}
		}
		if p.APIKey.Resolve != nil {
			if r := p.APIKey.Resolve(); r != nil {
				if r.Env == nil {
					r.Env = env
				}
				return r, nil
			}
		}
	}
	if key == "" {
		return nil, nil
	}
	source := "api_key"
	if p.APIKey != nil {
		source = p.APIKey.Name
	}
	return &Result{Auth: ModelAuth{APIKey: key}, Source: source, Env: env}, nil
}

func llamaResult(cred *Credential, existing *Result) *Result {
	url := strings.TrimSpace(os.Getenv("LLAMA_BASE_URL"))
	key := ""
	env := map[string]string{}
	source := "LLAMA_BASE_URL"
	if existing != nil {
		key = existing.Auth.APIKey
		if existing.Env != nil {
			env = existing.Env
		}
		if existing.Source != "" {
			source = existing.Source
		}
	}
	if cred != nil {
		if cred.Key != "" {
			key = cred.Key
		}
		if cred.Env != nil {
			if env == nil {
				env = map[string]string{}
			}
			for k, v := range cred.Env {
				env[k] = v
			}
			if v := strings.TrimSpace(cred.Env["LLAMA_BASE_URL"]); v != "" {
				url = v
			}
		}
	}
	if url == "" {
		return nil
	}
	if key == "" {
		key = os.Getenv("LLAMA_API_KEY")
	}
	if key == "" {
		key = "local"
	}
	norm, err := llama.NormalizeServerURL(url)
	if err != nil {
		return nil
	}
	if env == nil {
		env = map[string]string{}
	}
	env["LLAMA_BASE_URL"] = norm
	return &Result{
		Auth:   ModelAuth{APIKey: key, BaseURL: llama.InferenceURL(norm)},
		Env:    env,
		Source: source,
	}
}

func bedrockAmbientAuth() *Result {
	if os.Getenv("AWS_PROFILE") != "" {
		return &Result{Auth: ModelAuth{}, Source: "AWS_PROFILE"}
	}
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return &Result{Auth: ModelAuth{}, Source: "AWS_ACCESS_KEY_ID"}
	}
	if os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" || os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI") != "" {
		return &Result{Auth: ModelAuth{}, Source: "AWS_CONTAINER_CREDENTIALS"}
	}
	if os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != "" {
		return &Result{Auth: ModelAuth{}, Source: "AWS_WEB_IDENTITY_TOKEN_FILE"}
	}
	return nil
}
