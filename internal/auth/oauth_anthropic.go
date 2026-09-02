package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Ported from packages/ai/src/auth/oauth/anthropic.ts
var (
	anthropicClientID = mustB64("OWQxYzI1MGEtZTYxYi00NGQ5LTg4ZWQtNTk0NGQxOTYyZjVl")
	anthropicAuthURL  = "https://claude.ai/oauth/authorize"
	anthropicTokenURL = "https://platform.claude.com/v1/oauth/token"
	anthropicPort     = 53692
	anthropicPath     = "/callback"
	anthropicScopes   = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

func mustB64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type anthropicOAuth struct{}

func (anthropicOAuth) Name() string         { return "Anthropic (Claude Pro/Max)" }
func (anthropicOAuth) LoginLabel() string   { return "" }
func (anthropicOAuth) IsSubscription() bool { return true }

func (anthropicOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return Credential{}, err
	}
	host := callbackHost()
	srv, err := startCallback(ctx, host, anthropicPort, anthropicPath, verifier)
	if err != nil {
		return Credential{}, err
	}
	defer srv.Close()

	redirect := fmt.Sprintf("http://localhost:%d%s", anthropicPort, anthropicPath)
	q := url.Values{
		"code":                  {"true"},
		"client_id":             {anthropicClientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirect},
		"scope":                 {anthropicScopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {verifier},
	}
	notifyAuthURL(ix, anthropicAuthURL+"?"+q.Encode(),
		"Complete login in your browser. If the browser is on another machine, paste the final redirect URL here.")

	type result struct {
		code, state string
		err         error
	}
	ch := make(chan result, 2)
	go func() {
		c, st, err := srv.Wait()
		ch <- result{c, st, err}
	}()
	go func() {
		if ix.Prompt == nil {
			return
		}
		input, err := ix.Prompt(Prompt{
			Type:        PromptManualCode,
			Message:     "Complete login in your browser, or paste the authorization code / redirect URL here:",
			Placeholder: redirect,
		})
		if err != nil {
			ch <- result{err: err}
			srv.Cancel()
			return
		}
		code, state := parseAuthInput(input)
		ch <- result{code, state, nil}
		srv.Cancel()
	}()

	var code, state string
	select {
	case <-ctx.Done():
		return Credential{}, fmt.Errorf("login cancelled")
	case r := <-ch:
		if r.err != nil {
			return Credential{}, r.err
		}
		code, state = r.code, r.state
	}
	if code == "" {
		select {
		case <-ctx.Done():
			return Credential{}, fmt.Errorf("login cancelled")
		case r := <-ch:
			if r.err != nil {
				return Credential{}, r.err
			}
			code, state = r.code, r.state
		}
	}
	if code == "" {
		return Credential{}, fmt.Errorf("missing authorization code")
	}
	if state == "" {
		state = verifier
	}
	if state != verifier {
		return Credential{}, fmt.Errorf("oAuth state mismatch")
	}
	notifyProgress(ix, "Exchanging authorization code for tokens...")
	return exchangeAnthropicCode(ctx, code, state, verifier, redirect)
}

func (anthropicOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	body, err := postJSON(ctx, anthropicTokenURL, map[string]any{
		"grant_type":    "refresh_token",
		"client_id":     anthropicClientID,
		"refresh_token": cred.Refresh,
	})
	if err != nil {
		return Credential{}, fmt.Errorf("anthropic token refresh request failed: %w", err)
	}
	return parseAnthropicToken(body)
}

func (anthropicOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	return ModelAuth{
		APIKey: cred.Access,
		Headers: map[string]string{
			"Authorization": "Bearer " + cred.Access,
		},
	}, nil
}

func exchangeAnthropicCode(ctx context.Context, code, state, verifier, redirect string) (Credential, error) {
	body, err := postJSON(ctx, anthropicTokenURL, map[string]any{
		"grant_type":    "authorization_code",
		"client_id":     anthropicClientID,
		"code":          code,
		"state":         state,
		"redirect_uri":  redirect,
		"code_verifier": verifier,
	})
	if err != nil {
		return Credential{}, err
	}
	return parseAnthropicToken(body)
}

func parseAnthropicToken(body []byte) (Credential, error) {
	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Credential{}, fmt.Errorf("invalid token JSON: %w", err)
	}
	return Credential{
		Type:    TypeOAuth,
		Access:  data.AccessToken,
		Refresh: data.RefreshToken,
		Expires: time.Now().UnixMilli() + data.ExpiresIn*1000 - 5*60*1000,
	}, nil
}

func postJSON(ctx context.Context, rawURL string, payload map[string]any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hTTP request failed. status=%d; url=%s; body=%s", resp.StatusCode, rawURL, out)
	}
	return out, nil
}

func postForm(ctx context.Context, rawURL string, fields url.Values) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, stringsNewReader(fields.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return out, resp.StatusCode, nil
}

func stringsNewReader(s string) *bytes.Reader { return bytes.NewReader([]byte(s)) }
