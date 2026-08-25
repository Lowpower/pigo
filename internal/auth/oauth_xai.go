package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Ported from packages/ai/src/auth/oauth/xai.ts
const (
	xaiClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiScope    = "openid profile email offline_access grok-cli:access api:access"
)

var (
	xaiDeviceURL = "https://auth.x.ai/oauth2/device/code"
	xaiTokenURL  = "https://auth.x.ai/oauth2/token"
)

type xaiOAuth struct{}

func (xaiOAuth) Name() string         { return "xAI (Grok/X subscription)" }
func (xaiOAuth) LoginLabel() string   { return "Sign in with SuperGrok or X Premium" }
func (xaiOAuth) IsSubscription() bool { return true }

func (xaiOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	body, status, err := postForm(ctx, xaiDeviceURL, url.Values{
		"client_id": {xaiClientID},
		"scope":     {xaiScope},
		"referrer":  {"pi"},
	})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("xAI OAuth device authorization failed (HTTP %d): %s", status, body)
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return Credential{}, err
	}
	deviceCode, _ := data["device_code"].(string)
	userCode, _ := data["user_code"].(string)
	verURI, _ := data["verification_uri"].(string)
	verComplete, _ := data["verification_uri_complete"].(string)
	if _, err := trustedHTTPURL(verURI); err != nil {
		return Credential{}, fmt.Errorf("untrusted verification URI in xAI OAuth response")
	}
	uri := verURI
	if verComplete != "" {
		if u, err := trustedHTTPURL(verComplete); err == nil {
			uri = u
		}
	}
	interval, _ := data["interval"].(float64)
	expires, _ := data["expires_in"].(float64)
	notifyDevice(ix, userCode, uri, int(interval), int(expires))
	return pollDeviceCode(ctx, int(interval), int(expires), true, func() (devicePollResult[Credential], error) {
		b, st, err := postForm(ctx, xaiTokenURL, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {xaiClientID},
			"device_code": {deviceCode},
		})
		if err != nil {
			return devicePollResult[Credential]{}, err
		}
		var tok map[string]any
		_ = json.Unmarshal(b, &tok)
		if st >= 200 && st < 300 {
			c, err := xaiCreds(tok, "")
			if err != nil {
				return devicePollResult[Credential]{status: deviceFailed, message: err.Error()}, nil
			}
			return devicePollResult[Credential]{status: deviceComplete, value: c}, nil
		}
		errStr, _ := tok["error"].(string)
		switch errStr {
		case "authorization_pending":
			return devicePollResult[Credential]{status: devicePending}, nil
		case "slow_down":
			iv, _ := tok["interval"].(float64)
			return devicePollResult[Credential]{status: deviceSlowDown, intervalSeconds: int(iv)}, nil
		case "access_denied", "authorization_denied":
			return devicePollResult[Credential]{status: deviceFailed, message: "xAI device authorization was denied"}, nil
		case "expired_token":
			return devicePollResult[Credential]{status: deviceFailed, message: "xAI device code expired"}, nil
		}
		return devicePollResult[Credential]{status: deviceFailed, message: fmt.Sprintf("xAI OAuth device token polling failed (HTTP %d)", st)}, nil
	})
}

func (xaiOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	body, status, err := postForm(ctx, xaiTokenURL, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {xaiClientID},
		"refresh_token": {cred.Refresh},
	})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("xAI OAuth token refresh failed (HTTP %d): %s", status, body)
	}
	var tok map[string]any
	if err := json.Unmarshal(body, &tok); err != nil {
		return Credential{}, err
	}
	return xaiCreds(tok, cred.Refresh)
}

func (xaiOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	return ModelAuth{APIKey: cred.Access}, nil
}

func xaiCreds(body map[string]any, prevRefresh string) (Credential, error) {
	access, _ := body["access_token"].(string)
	if access == "" {
		return Credential{}, fmt.Errorf("invalid xAI OAuth response field: access_token")
	}
	refresh, _ := body["refresh_token"].(string)
	if refresh == "" {
		refresh = prevRefresh
	}
	if refresh == "" {
		return Credential{}, fmt.Errorf("invalid xAI OAuth response field: refresh_token")
	}
	exp, _ := body["expires_in"].(float64)
	if exp <= 0 {
		exp = 3600
	}
	return Credential{
		Type:    TypeOAuth,
		Access:  access,
		Refresh: refresh,
		Expires: time.Now().UnixMilli() + int64(exp)*1000 - 5*60*1000,
	}, nil
}
