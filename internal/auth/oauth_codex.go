package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Ported from packages/ai/src/auth/oauth/openai-codex.ts
const openaiCodexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

var (
	openaiCodexAuthURL   = "https://auth.openai.com/oauth/authorize"
	openaiCodexTokenURL  = "https://auth.openai.com/oauth/token"
	openaiCodexRedirect  = "http://localhost:1455/auth/callback"
	openaiCodexDeviceUC  = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	openaiCodexDeviceTok = "https://auth.openai.com/api/accounts/deviceauth/token"
	openaiCodexDeviceURI = "https://auth.openai.com/codex/device"
)

type openaiCodexOAuth struct{}

func (openaiCodexOAuth) Name() string         { return "OpenAI Codex (ChatGPT)" }
func (openaiCodexOAuth) LoginLabel() string   { return "" }
func (openaiCodexOAuth) IsSubscription() bool { return true }

func (o openaiCodexOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	method := "browser"
	if ix.Prompt != nil {
		sel, err := ix.Prompt(Prompt{
			Type:    PromptSelect,
			Message: "Select OpenAI Codex login method",
			Options: []SelectOption{
				{ID: "browser", Label: "browser"},
				{ID: "device_code", Label: "device_code"},
			},
		})
		if err != nil {
			return Credential{}, err
		}
		if sel != "" {
			method = sel
		}
	}
	if method == "device_code" {
		return o.loginDevice(ctx, ix)
	}
	return o.loginBrowser(ctx, ix)
}

func (openaiCodexOAuth) loginBrowser(ctx context.Context, ix Interaction) (Credential, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return Credential{}, err
	}
	state := randomHex(16)
	srv, err := startCallback(ctx, callbackHost(), 1455, "/auth/callback", state)
	if err != nil {
		return Credential{}, err
	}
	defer srv.Close()
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {openaiCodexClientID},
		"redirect_uri":          {openaiCodexRedirect},
		"scope":                 {"openid profile email offline_access"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	notifyAuthURL(ix, openaiCodexAuthURL+"?"+q.Encode(),
		"Complete login in your browser. If the browser is on another machine, paste the final redirect URL here.")
	code, _, err := waitCodeOrPaste(ix, srv, openaiCodexRedirect)
	if err != nil {
		return Credential{}, err
	}
	if code == "" {
		return Credential{}, fmt.Errorf("missing authorization code")
	}
	tok, err := exchangeCodexCode(ctx, code, verifier, openaiCodexRedirect)
	if err != nil {
		return Credential{}, err
	}
	return tok, nil
}

func (openaiCodexOAuth) loginDevice(ctx context.Context, ix Interaction) (Credential, error) {
	body, err := postJSON(ctx, openaiCodexDeviceUC, map[string]any{"client_id": openaiCodexClientID})
	if err != nil {
		return Credential{}, err
	}
	var dev struct {
		DeviceAuthID    string  `json:"device_auth_id"`
		UserCode        string  `json:"user_code"`
		IntervalSeconds float64 `json:"interval"`
	}
	if err := json.Unmarshal(body, &dev); err != nil {
		return Credential{}, err
	}
	notifyDevice(ix, dev.UserCode, openaiCodexDeviceURI, int(dev.IntervalSeconds), 15*60)
	tok, err := pollDeviceCode(ctx, int(dev.IntervalSeconds), 15*60, true, func() (devicePollResult[Credential], error) {
		b, err := postJSON(ctx, openaiCodexDeviceTok, map[string]any{
			"device_auth_id": dev.DeviceAuthID,
			"client_id":      openaiCodexClientID,
		})
		if err != nil {
			if strings.Contains(err.Error(), "status=404") || strings.Contains(strings.ToLower(err.Error()), "pending") {
				return devicePollResult[Credential]{status: devicePending}, nil
			}
			return devicePollResult[Credential]{}, err
		}
		var data struct {
			AuthorizationCode string `json:"authorization_code"`
			CodeVerifier      string `json:"code_verifier"`
		}
		if json.Unmarshal(b, &data) != nil || data.AuthorizationCode == "" {
			return devicePollResult[Credential]{status: devicePending}, nil
		}
		cred, err := exchangeCodexCode(ctx, data.AuthorizationCode, data.CodeVerifier, "https://auth.openai.com/deviceauth/callback")
		if err != nil {
			return devicePollResult[Credential]{status: deviceFailed, message: err.Error()}, nil
		}
		return devicePollResult[Credential]{status: deviceComplete, value: cred}, nil
	})
	return tok, err
}

func (openaiCodexOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	body, status, err := postForm(ctx, openaiCodexTokenURL, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.Refresh},
		"client_id":     {openaiCodexClientID},
	})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("openAI Codex token refresh failed (%d): %s", status, body)
	}
	return parseCodexToken(body, cred)
}

func (openaiCodexOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	headers := map[string]string{
		"originator":  "pi",
		"OpenAI-Beta": "responses=experimental",
	}
	id := cred.extraString("accountId")
	if id == "" {
		id = codexAccountID(cred.Access)
	}
	if id != "" {
		headers["chatgpt-account-id"] = id
	}
	return ModelAuth{APIKey: cred.Access, Headers: headers}, nil
}

func exchangeCodexCode(ctx context.Context, code, verifier, redirect string) (Credential, error) {
	body, status, err := postForm(ctx, openaiCodexTokenURL, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {openaiCodexClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirect},
	})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("openAI Codex token exchange failed (%d): %s", status, body)
	}
	return parseCodexToken(body, Credential{})
}

func parseCodexToken(body []byte, prev Credential) (Credential, error) {
	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Credential{}, err
	}
	if data.AccessToken == "" || data.RefreshToken == "" {
		return Credential{}, fmt.Errorf("openAI Codex token response missing fields")
	}
	c := Credential{
		Type:    TypeOAuth,
		Access:  data.AccessToken,
		Refresh: data.RefreshToken,
		Expires: time.Now().UnixMilli() + data.ExpiresIn*1000,
	}
	if id := codexAccountID(data.AccessToken); id != "" {
		c = c.withExtra("accountId", id)
	} else if prev.extraString("accountId") != "" {
		c = c.withExtra("accountId", prev.extraString("accountId"))
	}
	return c, nil
}

func codexAccountID(access string) string {
	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		b, err2 := base64.StdEncoding.DecodeString(parts[1])
		if err2 != nil {
			return ""
		}
		payload = b
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	id, _ := auth["chatgpt_account_id"].(string)
	return id
}

func waitCodeOrPaste(ix Interaction, srv *callbackServer, placeholder string) (code, state string, err error) {
	ctx := ix.ctx()
	ch := make(chan struct {
		code, state string
		err         error
	}, 2)
	go func() {
		c, st, e := srv.Wait()
		ch <- struct {
			code, state string
			err         error
		}{c, st, e}
	}()
	if ix.Prompt != nil {
		go func() {
			input, e := ix.Prompt(Prompt{
				Type:        PromptManualCode,
				Message:     "Complete login in your browser, or paste the authorization code / redirect URL here:",
				Placeholder: placeholder,
			})
			if e != nil {
				ch <- struct {
					code, state string
					err         error
				}{err: e}
				srv.Cancel()
				return
			}
			c, st := parseAuthInput(input)
			ch <- struct {
				code, state string
				err         error
			}{c, st, nil}
			srv.Cancel()
		}()
	}
	select {
	case <-ctx.Done():
		return "", "", fmt.Errorf("login cancelled")
	case r := <-ch:
		return r.code, r.state, r.err
	}
}
