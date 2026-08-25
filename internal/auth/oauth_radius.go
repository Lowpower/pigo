package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Ported from packages/ai/src/auth/oauth/radius.ts
const radiusClientID = "pi-gateway"

type radiusOAuth struct {
	name    string
	gateway string
}

// NewRadiusOAuth builds a Radius flow for a gateway base URL.
func NewRadiusOAuth(name, gateway string) OAuth {
	g := strings.TrimRight(gateway, "/")
	if name == "" {
		name = "Radius"
	}
	return radiusOAuth{name: name, gateway: g}
}

func (r radiusOAuth) Name() string         { return r.name }
func (r radiusOAuth) LoginLabel() string   { return "" }
func (r radiusOAuth) IsSubscription() bool { return false }

func (r radiusOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	method := "browser"
	if ix.Prompt != nil {
		sel, err := ix.Prompt(Prompt{
			Type:    PromptSelect,
			Message: "Select Radius login method",
			Options: []SelectOption{
				{ID: "browser", Label: "browser"},
				{ID: "device-code", Label: "device-code"},
			},
		})
		if err != nil {
			return Credential{}, err
		}
		if sel != "" {
			method = sel
		}
	}
	if method == "device-code" {
		return r.loginDevice(ctx, ix)
	}
	return r.loginBrowser(ctx, ix)
}

func (r radiusOAuth) loginBrowser(ctx context.Context, ix Interaction) (Credential, error) {
	disc, err := loadRadiusDiscovery(ctx, r.gateway)
	if err != nil {
		return Credential{}, err
	}
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return Credential{}, err
	}
	state := randomHex(16)
	srv, err := startCallback(ctx, "127.0.0.1", 1456, "/oauth/callback", state)
	if err != nil {
		return Credential{}, err
	}
	defer srv.Close()
	redirect := "http://127.0.0.1:1456/oauth/callback"
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {radiusClientID},
		"redirect_uri":          {redirect},
		"scope":                 {"gateway offline_access"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	notifyAuthURL(ix, disc+"?"+q.Encode(), "Complete Radius login in your browser.")
	code, _, err := waitCodeOrPaste(ix, srv, redirect)
	if err != nil {
		return Credential{}, err
	}
	if code == "" {
		return Credential{}, fmt.Errorf("missing authorization code")
	}
	return r.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {radiusClientID},
		"code":          {code},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
}

func (r radiusOAuth) loginDevice(ctx context.Context, ix Interaction) (Credential, error) {
	body, status, err := postForm(ctx, r.gateway+"/v1/oauth/device", url.Values{
		"client_id": {radiusClientID},
		"scope":     {"gateway offline_access"},
	})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("radius device authorization failed (%d): %s", status, body)
	}
	var dev struct {
		DeviceCode      string  `json:"device_code"`
		UserCode        string  `json:"user_code"`
		VerificationURI string  `json:"verification_uri"`
		ExpiresIn       float64 `json:"expires_in"`
		Interval        float64 `json:"interval"`
	}
	if err := json.Unmarshal(body, &dev); err != nil {
		return Credential{}, err
	}
	notifyDevice(ix, dev.UserCode, dev.VerificationURI, int(dev.Interval), int(dev.ExpiresIn))
	return pollDeviceCode(ctx, int(dev.Interval), int(dev.ExpiresIn), true, func() (devicePollResult[Credential], error) {
		c, err := r.token(ctx, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {radiusClientID},
			"device_code": {dev.DeviceCode},
		})
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "authorization_pending") {
				return devicePollResult[Credential]{status: devicePending}, nil
			}
			if strings.Contains(msg, "slow_down") {
				return devicePollResult[Credential]{status: deviceSlowDown}, nil
			}
			return devicePollResult[Credential]{status: deviceFailed, message: msg}, nil
		}
		return devicePollResult[Credential]{status: deviceComplete, value: c}, nil
	})
}

func (r radiusOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	next, err := r.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {radiusClientID},
		"refresh_token": {cred.Refresh},
	})
	if err != nil {
		return Credential{}, err
	}
	if scope := cred.extraString("scope"); scope != "" && next.extraString("scope") == "" {
		next = next.withExtra("scope", scope)
	}
	return next, nil
}

func (r radiusOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	return ModelAuth{APIKey: cred.Access, BaseURL: r.gateway}, nil
}

func (r radiusOAuth) token(ctx context.Context, fields url.Values) (Credential, error) {
	body, status, err := postForm(ctx, r.gateway+"/v1/oauth/token", fields)
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("radius OAuth token request failed: %s", body)
	}
	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Credential{}, err
	}
	c := Credential{
		Type:    TypeOAuth,
		Access:  data.AccessToken,
		Refresh: data.RefreshToken,
		Expires: time.Now().UnixMilli() + data.ExpiresIn*1000 - 60*1000,
	}
	if data.Scope != "" {
		c = c.withExtra("scope", data.Scope)
	}
	return c, nil
}

func loadRadiusDiscovery(ctx context.Context, gateway string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway+"/v1/oauth", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("could not load Radius OAuth config from %s: %d %s", gateway, resp.StatusCode, b)
	}
	var d struct {
		AuthorizationEndpoint string `json:"authorizationEndpoint"`
	}
	if err := json.Unmarshal(b, &d); err != nil || d.AuthorizationEndpoint == "" {
		return "", fmt.Errorf("invalid Radius OAuth config from %s", gateway)
	}
	return d.AuthorizationEndpoint, nil
}

// AttachRadius registers a radius OAuth provider (models.json oauth:"radius").
func AttachRadius(id, name, gateway string) {
	registerProvider(Provider{ID: id, OAuth: NewRadiusOAuth(name, gateway)})
}
