package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	metaClientID    = "1031625952748946"
	metaKeyLifetime = 24 * time.Hour
	metaAPIVersion  = "1.0.0"
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
	cred, err := loginMeta(ix)
	if err != nil && ix.ctx().Err() != nil {
		return Credential{}, errors.New(deviceCancelMessage)
	}
	return cred, err
}

func (metaOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	if strings.TrimSpace(cred.Refresh) == "" {
		return Credential{}, errors.New("meta session expired. run /login meta to sign in again")
	}
	return mintMetaKey(ctx, cred.Refresh)
}

func (metaOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	if strings.TrimSpace(cred.Access) == "" {
		return ModelAuth{}, errors.New("meta api key missing")
	}
	return ModelAuth{APIKey: cred.Access}, nil
}

func loginMeta(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	dev, err := startMetaDevice(ctx)
	if err != nil {
		return Credential{}, err
	}
	notifyDevice(ix, dev.userCode, dev.verificationURI, dev.interval, dev.expires)
	token, err := pollMetaIdentity(ctx, dev)
	if err != nil {
		return Credential{}, err
	}
	notifyProgress(ix, "Enabling Meta Model API access...")
	return mintMetaKey(ctx, token)
}

type metaDevice struct {
	deviceCode      string
	userCode        string
	verificationURI string
	interval        int
	expires         int
}

func startMetaDevice(ctx context.Context) (metaDevice, error) {
	body, status, err := postForm(ctx, metaDeviceURL, url.Values{"client_id": {metaClientID}})
	if err != nil {
		return metaDevice{}, err
	}
	payload := decodeMetaJSON(body)
	if status < 200 || status >= 300 {
		return metaDevice{}, fmt.Errorf("meta device authorization failed with status %d%s", status, metaErrorDetail(payload))
	}
	deviceCode, _ := payload["device_code"].(string)
	userCode, _ := payload["user_code"].(string)
	verification := metaTrustedURL(payload["verification_uri_complete"])
	if verification == "" {
		verification = metaTrustedURL(payload["verification_uri"])
	}
	if deviceCode == "" || userCode == "" || verification == "" {
		return metaDevice{}, fmt.Errorf("invalid meta device authorization response")
	}
	return metaDevice{
		deviceCode:      deviceCode,
		userCode:        userCode,
		verificationURI: verification,
		interval:        metaPositiveInt(payload["interval"]),
		expires:         metaPositiveInt(payload["expires_in"]),
	}, nil
}

func pollMetaIdentity(ctx context.Context, dev metaDevice) (string, error) {
	return pollDeviceCode(ctx, dev.interval, dev.expires, true, func() (devicePollResult[string], error) {
		body, status, err := postForm(ctx, metaTokenURL, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {dev.deviceCode},
			"client_id":   {metaClientID},
		})
		if err != nil {
			return devicePollResult[string]{}, err
		}
		payload := decodeMetaJSON(body)
		if status >= 200 && status < 300 {
			if token, _ := payload["access_token"].(string); token != "" {
				return devicePollResult[string]{status: deviceComplete, value: token}, nil
			}
		}
		switch payload["error"] {
		case "authorization_pending":
			return devicePollResult[string]{status: devicePending}, nil
		case "slow_down":
			return devicePollResult[string]{status: deviceSlowDown, intervalSeconds: metaPositiveInt(payload["interval"])}, nil
		case "access_denied":
			return devicePollResult[string]{status: deviceFailed, message: "meta login was denied"}, nil
		case "expired_token":
			return devicePollResult[string]{status: deviceFailed, message: "meta device authorization expired. please restart login"}, nil
		default:
			return devicePollResult[string]{
				status:  deviceFailed,
				message: fmt.Sprintf("meta device token request failed with status %d%s", status, metaErrorDetail(payload)),
			}, nil
		}
	})
}

func mintMetaKey(ctx context.Context, identity string) (Credential, error) {
	body, status, err := metaMint(ctx, identity)
	if err != nil {
		return Credential{}, err
	}
	payload := decodeMetaJSON(body)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return Credential{}, fmt.Errorf("meta session expired (status %d). run /login meta to sign in again%s", status, metaErrorDetail(payload))
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("meta api key mint failed with status %d%s", status, metaErrorDetail(payload))
	}
	key, _ := payload["api_key"].(string)
	if strings.TrimSpace(key) == "" {
		msg := "meta did not issue an api key"
		if action := metaTrustedURL(payload["action_url"]); action != "" {
			msg += ". complete setup at " + action
		}
		return Credential{}, errors.New(msg)
	}
	return Credential{
		Type:    TypeOAuth,
		Access:  key,
		Refresh: identity,
		Expires: time.Now().Add(metaKeyLifetime).UnixMilli(),
	}, nil
}

func metaMint(ctx context.Context, identity string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, metaMintURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	req.Header.Set("authorization", "Bearer "+identity)
	req.Header.Set("x-api-version", metaAPIVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return out, resp.StatusCode, nil
}

func decodeMetaJSON(body []byte) map[string]any {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return map[string]any{}
	}
	return payload
}

func metaErrorDetail(payload map[string]any) string {
	for _, key := range []string{"error_description", "detail", "message", "error"} {
		s, _ := payload[key].(string)
		s = strings.TrimSpace(s)
		if s != "" {
			return ": " + s
		}
	}
	return ""
}

func metaTrustedURL(v any) string {
	s, _ := v.(string)
	u, err := trustedHTTPURL(s)
	if err != nil {
		return ""
	}
	return u
}

func metaPositiveInt(v any) int {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return int(n)
		}
	case int:
		if n > 0 {
			return n
		}
	}
	return 0
}
