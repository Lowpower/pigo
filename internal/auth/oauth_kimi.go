package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"
)

// Ported from packages/ai/src/auth/oauth/kimi-coding.ts
const kimiClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"

type kimiOAuth struct{}

func (kimiOAuth) Name() string         { return "Kimi Code (subscription)" }
func (kimiOAuth) LoginLabel() string   { return "Sign in with Kimi Code" }
func (kimiOAuth) IsSubscription() bool { return true }

func kimiHost() string {
	h := os.Getenv("KIMI_CODE_OAUTH_HOST")
	if h == "" {
		h = os.Getenv("KIMI_OAUTH_HOST")
	}
	if h == "" {
		h = "https://auth.kimi.com"
	}
	for len(h) > 0 && h[len(h)-1] == '/' {
		h = h[:len(h)-1]
	}
	return h
}

func (kimiOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	host := kimiHost()
	body, status, err := postForm(ctx, host+"/api/oauth/device_authorization", url.Values{"client_id": {kimiClientID}})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("kimi Code device authorization failed with status %d: %s", status, body)
	}
	var dev struct {
		DeviceCode              string  `json:"device_code"`
		UserCode                string  `json:"user_code"`
		VerificationURI         string  `json:"verification_uri"`
		VerificationURIComplete string  `json:"verification_uri_complete"`
		Interval                float64 `json:"interval"`
		ExpiresIn               float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &dev); err != nil {
		return Credential{}, err
	}
	if _, err := trustedHTTPURL(dev.VerificationURI); err != nil {
		return Credential{}, fmt.Errorf("invalid Kimi Code device authorization response")
	}
	interval := int(dev.Interval)
	expires := int(dev.ExpiresIn)
	if interval <= 0 {
		interval = 5
	}
	if expires <= 0 {
		expires = 15 * 60
	}
	uri := dev.VerificationURIComplete
	if uri == "" {
		uri = dev.VerificationURI
	}
	notifyDevice(ix, dev.UserCode, uri, interval, expires)
	tok, err := pollDeviceCode(ctx, interval, expires, true, func() (devicePollResult[Credential], error) {
		b, st, err := postForm(ctx, host+"/api/oauth/token", url.Values{
			"client_id":   {kimiClientID},
			"device_code": {dev.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		})
		if err != nil {
			return devicePollResult[Credential]{}, err
		}
		return kimiPollToken(b, st)
	})
	return tok, err
}

func kimiPollToken(body []byte, status int) (devicePollResult[Credential], error) {
	var data map[string]any
	_ = json.Unmarshal(body, &data)
	if status >= 200 && status < 300 {
		c, err := parseKimiToken(data)
		if err != nil {
			return devicePollResult[Credential]{status: deviceFailed, message: err.Error()}, nil
		}
		return devicePollResult[Credential]{status: deviceComplete, value: c}, nil
	}
	errStr, _ := data["error"].(string)
	switch errStr {
	case "authorization_pending":
		return devicePollResult[Credential]{status: devicePending}, nil
	case "slow_down":
		iv, _ := data["interval"].(float64)
		return devicePollResult[Credential]{status: deviceSlowDown, intervalSeconds: int(iv)}, nil
	case "expired_token":
		return devicePollResult[Credential]{status: deviceFailed, message: "Kimi Code device authorization expired. Please restart login."}, nil
	case "access_denied":
		return devicePollResult[Credential]{status: deviceFailed, message: "Kimi Code login was denied."}, nil
	}
	return devicePollResult[Credential]{status: deviceFailed, message: fmt.Sprintf("Kimi Code device token request failed (status %d)", status)}, nil
}

func parseKimiToken(data map[string]any) (Credential, error) {
	access, _ := data["access_token"].(string)
	refresh, _ := data["refresh_token"].(string)
	exp, _ := data["expires_in"].(float64)
	if access == "" || refresh == "" || exp <= 0 {
		return Credential{}, fmt.Errorf("kimi Code token response missing fields")
	}
	return Credential{Type: TypeOAuth, Access: access, Refresh: refresh, Expires: time.Now().UnixMilli() + int64(exp)*1000}, nil
}

func (kimiOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	var last error
	for attempt := 0; attempt <= 3; attempt++ {
		if attempt > 0 {
			if err := abortableSleep(ctx, time.Duration(1<<uint(attempt-1))*time.Second); err != nil {
				return Credential{}, err
			}
		}
		body, status, err := postForm(ctx, kimiHost()+"/api/oauth/token", url.Values{
			"client_id":     {kimiClientID},
			"grant_type":    {"refresh_token"},
			"refresh_token": {cred.Refresh},
		})
		if err != nil {
			last = err
			continue
		}
		var data map[string]any
		_ = json.Unmarshal(body, &data)
		if status >= 200 && status < 300 {
			return parseKimiToken(data)
		}
		errStr, _ := data["error"].(string)
		if status == 401 || status == 403 || errStr == "invalid_grant" {
			return Credential{}, fmt.Errorf("kimi Code token refresh unauthorized (status %d)", status)
		}
		if (status == 429 || status >= 500) && attempt < 3 {
			last = fmt.Errorf("kimi Code token refresh failed with status %d", status)
			continue
		}
		return Credential{}, fmt.Errorf("kimi Code token refresh failed with status %d: %s", status, body)
	}
	if last == nil {
		last = fmt.Errorf("kimi Code token refresh failed")
	}
	return Credential{}, last
}

func (kimiOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	return ModelAuth{Headers: map[string]string{"Authorization": "Bearer " + cred.Access}}, nil
}
