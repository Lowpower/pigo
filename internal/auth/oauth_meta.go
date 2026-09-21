package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	metaClientID       = "1031625952748946"
	metaAPIKeyLifetime = 24 * time.Hour
	metaAPIVersion     = "1.0.0"
)

var (
	metaDeviceURL = "https://auth.meta.com/oidc/device/authorization/"
	metaTokenURL  = "https://auth.meta.com/oidc/device/token/"
	metaMintURL   = "https://api.meta.ai/muse-code/key"
)

type metaOAuth struct{}

func (metaOAuth) Name() string         { return "Meta (Muse subscription)" }
func (metaOAuth) LoginLabel() string   { return "Sign in with Meta" }
func (metaOAuth) IsSubscription() bool { return true }

func (metaOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	body, status, err := postForm(ctx, metaDeviceURL, url.Values{"client_id": {metaClientID}})
	if err != nil {
		return Credential{}, err
	}
	var data map[string]any
	_ = json.Unmarshal(body, &data)
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("meta device authorization failed with status %d%s", status, metaErrorDetail(data))
	}
	deviceCode, _ := data["device_code"].(string)
	userCode, _ := data["user_code"].(string)
	verComplete, _ := data["verification_uri_complete"].(string)
	verURI, _ := data["verification_uri"].(string)
	uri, uriErr := trustedHTTPURL(verComplete)
	if uriErr != nil {
		uri, uriErr = trustedHTTPURL(verURI)
	}
	if deviceCode == "" || userCode == "" || uriErr != nil {
		return Credential{}, fmt.Errorf("invalid Meta device authorization response")
	}
	interval, _ := data["interval"].(float64)
	expires, _ := data["expires_in"].(float64)
	notifyDevice(ix, userCode, uri, int(interval), int(expires))
	identity, err := pollDeviceCode(ctx, int(interval), int(expires), true, func() (devicePollResult[string], error) {
		b, st, err := postForm(ctx, metaTokenURL, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {metaClientID},
		})
		if err != nil {
			return devicePollResult[string]{}, err
		}
		return metaPollIdentity(b, st)
	})
	if err != nil {
		return Credential{}, err
	}
	notifyProgress(ix, "Enabling Meta Model API access...")
	return mintMetaAPIKey(ctx, identity)
}

func metaPollIdentity(body []byte, status int) (devicePollResult[string], error) {
	var data map[string]any
	_ = json.Unmarshal(body, &data)
	if status >= 200 && status < 300 {
		tok, _ := data["access_token"].(string)
		if tok == "" {
			return devicePollResult[string]{status: deviceFailed, message: "invalid Meta device token response"}, nil
		}
		return devicePollResult[string]{status: deviceComplete, value: tok}, nil
	}
	errStr, _ := data["error"].(string)
	switch errStr {
	case "authorization_pending":
		return devicePollResult[string]{status: devicePending}, nil
	case "slow_down":
		iv, _ := data["interval"].(float64)
		return devicePollResult[string]{status: deviceSlowDown, intervalSeconds: int(iv)}, nil
	case "access_denied":
		return devicePollResult[string]{status: deviceFailed, message: "Meta login was denied."}, nil
	case "expired_token":
		return devicePollResult[string]{status: deviceFailed, message: "Meta device authorization expired. Please restart login."}, nil
	}
	return devicePollResult[string]{status: deviceFailed, message: fmt.Sprintf("Meta device token request failed with status %d%s", status, metaErrorDetail(data))}, nil
}

func (metaOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	return mintMetaAPIKey(ctx, cred.Refresh)
}

func (metaOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	return ModelAuth{APIKey: cred.Access}, nil
}

func mintMetaAPIKey(ctx context.Context, identityToken string) (Credential, error) {
	if identityToken == "" {
		return Credential{}, fmt.Errorf("meta session expired (status 401). Run `/login meta` to sign in again")
	}
	body, status, err := postJSONHeaders(ctx, metaMintURL, map[string]string{
		"authorization": "Bearer " + identityToken,
		"x-api-version": metaAPIVersion,
	}, []byte("{}"))
	if err != nil {
		return Credential{}, err
	}
	var data map[string]any
	_ = json.Unmarshal(body, &data)
	if status == 401 || status == 403 {
		return Credential{}, fmt.Errorf("meta session expired (status %d). Run `/login meta` to sign in again%s", status, metaErrorDetail(data))
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("meta API key mint failed with status %d%s", status, metaErrorDetail(data))
	}
	apiKey, _ := data["api_key"].(string)
	if apiKey == "" {
		msg := "Meta did not issue an API key."
		if s, ok := data["action_url"].(string); ok {
			if action, err := trustedHTTPURL(s); err == nil {
				msg += " Complete setup at " + action
			}
		}
		return Credential{}, fmt.Errorf("%s", msg)
	}
	return Credential{
		Type:    TypeOAuth,
		Access:  apiKey,
		Refresh: identityToken,
		Expires: time.Now().UnixMilli() + metaAPIKeyLifetime.Milliseconds(),
	}, nil
}

func metaErrorDetail(data map[string]any) string {
	for _, key := range []string{"error_description", "detail", "message", "error"} {
		if s, _ := data[key].(string); s != "" {
			return ": " + s
		}
	}
	return ""
}

func postJSONHeaders(ctx context.Context, rawURL string, headers map[string]string, payload []byte) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return out, resp.StatusCode, nil
}
