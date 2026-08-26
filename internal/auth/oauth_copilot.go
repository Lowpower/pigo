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

// Ported from packages/ai/src/auth/oauth/github-copilot.ts
var copilotClientID = mustB64("SXYxLmI1MDdhMDhjODdlY2ZlOTg=")

type githubCopilotOAuth struct{}

func (githubCopilotOAuth) Name() string         { return "GitHub Copilot" }
func (githubCopilotOAuth) LoginLabel() string   { return "" }
func (githubCopilotOAuth) IsSubscription() bool { return true }

func copilotDomain(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "github.com", nil
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("invalid GitHub Enterprise URL/domain")
	}
	return u.Hostname(), nil
}

func (githubCopilotOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	input := ""
	if ix.Prompt != nil {
		var err error
		input, err = ix.Prompt(Prompt{
			Type:        PromptText,
			Message:     "GitHub Enterprise URL/domain (blank for github.com)",
			Placeholder: "company.ghe.com",
		})
		if err != nil {
			return Credential{}, err
		}
	}
	domain, err := copilotDomain(input)
	if err != nil {
		return Credential{}, err
	}
	enterprise := ""
	if domain != "github.com" {
		enterprise = domain
	}
	body, status, err := postForm(ctx, fmt.Sprintf("https://%s/login/device/code", domain), url.Values{
		"client_id": {copilotClientID},
		"scope":     {"read:user"},
	})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("gitHub device code failed (%d): %s", status, body)
	}
	var dev struct {
		DeviceCode      string  `json:"device_code"`
		UserCode        string  `json:"user_code"`
		VerificationURI string  `json:"verification_uri"`
		Interval        float64 `json:"interval"`
		ExpiresIn       float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &dev); err != nil {
		return Credential{}, err
	}
	if _, err := trustedHTTPURL(dev.VerificationURI); err != nil {
		return Credential{}, fmt.Errorf("untrusted verification_uri in device code response")
	}
	notifyDevice(ix, dev.UserCode, dev.VerificationURI, int(dev.Interval), int(dev.ExpiresIn))
	ghToken, err := pollDeviceCode(ctx, int(dev.Interval), int(dev.ExpiresIn), true, func() (devicePollResult[string], error) {
		b, st, err := postForm(ctx, fmt.Sprintf("https://%s/login/oauth/access_token", domain), url.Values{
			"client_id":   {copilotClientID},
			"device_code": {dev.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		})
		if err != nil {
			return devicePollResult[string]{}, err
		}
		var data map[string]any
		_ = json.Unmarshal(b, &data)
		if tok, _ := data["access_token"].(string); tok != "" && st >= 200 && st < 300 {
			return devicePollResult[string]{status: deviceComplete, value: tok}, nil
		}
		errStr, _ := data["error"].(string)
		switch errStr {
		case "authorization_pending":
			return devicePollResult[string]{status: devicePending}, nil
		case "slow_down":
			iv, _ := data["interval"].(float64)
			return devicePollResult[string]{status: deviceSlowDown, intervalSeconds: int(iv)}, nil
		case "expired_token":
			return devicePollResult[string]{status: deviceFailed, message: "GitHub device code expired"}, nil
		case "access_denied":
			return devicePollResult[string]{status: deviceFailed, message: "GitHub login was denied"}, nil
		}
		if st >= 200 && st < 300 {
			return devicePollResult[string]{status: devicePending}, nil
		}
		return devicePollResult[string]{status: deviceFailed, message: fmt.Sprintf("GitHub token poll failed (%d)", st)}, nil
	})
	if err != nil {
		return Credential{}, err
	}
	cred, err := refreshCopilotAccess(ctx, ghToken, enterprise)
	if err != nil {
		return Credential{}, err
	}
	if enterprise != "" {
		cred = cred.withExtra("enterpriseUrl", enterprise)
	}
	return cred, nil
}

func (githubCopilotOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	ent := cred.extraString("enterpriseUrl")
	next, err := refreshCopilotAccess(ctx, cred.Refresh, ent)
	if err != nil {
		return Credential{}, err
	}
	if ent != "" {
		next = next.withExtra("enterpriseUrl", ent)
	}
	if ids := cred.Extra["availableModelIds"]; ids != nil {
		next = next.withExtra("availableModelIds", ids)
	}
	return next, nil
}

func (githubCopilotOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	return ModelAuth{
		APIKey:  cred.Access,
		BaseURL: copilotBaseURL(cred.Access, cred.extraString("enterpriseUrl")),
	}, nil
}

func refreshCopilotAccess(ctx context.Context, githubToken, enterprise string) (Credential, error) {
	domain := "github.com"
	if enterprise != "" {
		domain = enterprise
	}
	u := fmt.Sprintf("https://api.%s/copilot_internal/v2/token", domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("authorization", "token "+githubToken)
	req.Header.Set("user-agent", "GitHubCopilotChat/0.35.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credential{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("copilot token exchange failed (%d): %s", resp.StatusCode, b)
	}
	var data struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return Credential{}, err
	}
	exp := data.ExpiresAt * 1000
	if exp == 0 {
		exp = time.Now().UnixMilli() + 3600*1000
	}
	return Credential{
		Type:    TypeOAuth,
		Access:  data.Token,
		Refresh: githubToken,
		Expires: exp - 5*60*1000,
	}, nil
}

func copilotBaseURL(token, enterprise string) string {
	if i := strings.Index(token, "proxy-ep="); i >= 0 {
		rest := token[i+len("proxy-ep="):]
		if j := strings.IndexByte(rest, ';'); j >= 0 {
			rest = rest[:j]
		}
		host := strings.Replace(rest, "proxy.", "api.", 1)
		if host != "" {
			return "https://" + host
		}
	}
	if enterprise != "" {
		return "https://copilot-api." + enterprise
	}
	return "https://api.individual.githubcopilot.com"
}
