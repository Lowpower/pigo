package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Ported from packages/ai/src/auth/oauth/openrouter.ts
var (
	openrouterAuthURL  = "https://openrouter.ai/auth"
	openrouterTokenURL = "https://openrouter.ai/api/v1/auth/keys"
)

type openrouterOAuth struct{}

func (openrouterOAuth) Name() string         { return "OpenRouter OAuth" }
func (openrouterOAuth) LoginLabel() string   { return "Sign in with OpenRouter" }
func (openrouterOAuth) IsSubscription() bool { return false }

func (openrouterOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return Credential{}, err
	}
	path := "/oauth/callback/" + randomHex(16)
	srv, err := startCallbackEphemeral(ctx, callbackHost(), path)
	if err != nil {
		return Credential{}, err
	}
	defer srv.Close()
	callbackURL := srv.URL(path)
	q := url.Values{
		"callback_url":          {callbackURL},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	notifyProgress(ix, "Listening for OpenRouter OAuth callback on "+callbackURL)
	notifyAuthURL(ix, openrouterAuthURL+"?"+q.Encode(),
		"Complete sign-in in your browser. If the browser is on another machine, paste the final redirect URL here.")

	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 2)
	go func() {
		c, _, err := srv.Wait()
		ch <- result{c, err}
	}()
	go func() {
		if ix.Prompt == nil {
			return
		}
		input, err := ix.Prompt(Prompt{
			Type:        PromptManualCode,
			Message:     "Complete sign-in in your browser, or paste the authorization code / redirect URL here:",
			Placeholder: callbackURL,
		})
		if err != nil {
			ch <- result{err: err}
			srv.Cancel()
			return
		}
		code, _ := parseAuthInput(input)
		ch <- result{code, nil}
		srv.Cancel()
	}()
	var code string
	select {
	case <-ctx.Done():
		return Credential{}, fmt.Errorf("login cancelled")
	case r := <-ch:
		if r.err != nil {
			return Credential{}, r.err
		}
		code = r.code
	}
	if code == "" {
		return Credential{}, fmt.Errorf("missing authorization code")
	}
	notifyProgress(ix, "Exchanging authorization code for an API key...")
	return exchangeOpenRouter(ctx, code, verifier)
}

func (openrouterOAuth) Refresh(_ context.Context, cred Credential) (Credential, error) {
	return cred, nil
}

func (openrouterOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	return ModelAuth{APIKey: cred.Access}, nil
}

func exchangeOpenRouter(ctx context.Context, code, verifier string) (Credential, error) {
	body, err := postJSON(ctx, openrouterTokenURL, map[string]any{
		"code":                  code,
		"code_verifier":         verifier,
		"code_challenge_method": "S256",
	})
	if err != nil {
		return Credential{}, err
	}
	var data struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &data); err != nil || data.Key == "" {
		return Credential{}, fmt.Errorf("openRouter OAuth response carries no key")
	}
	return Credential{Type: TypeOAuth, Access: data.Key, Refresh: "", Expires: 1<<62 - 1}, nil
}
