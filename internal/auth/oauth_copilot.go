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

	"github.com/Lowpower/pigo/internal/models"
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
	if extraStringSlice(next, "availableModelIds") == nil {
		if ids := extraStringSlice(cred, "availableModelIds"); ids != nil {
			next = next.withExtra("availableModelIds", ids)
		}
	}
	applyCopilotAvailableModels(next)
	return next, nil
}

func (githubCopilotOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	applyCopilotAvailableModels(cred)
	return ModelAuth{
		APIKey:  cred.Access,
		BaseURL: copilotBaseURL(cred.Access, cred.extraString("enterpriseUrl")),
	}, nil
}

var (
	copilotTokenURL      string
	copilotModelsBaseURL string
)

const copilotAPIVersion = "2026-06-01"

func copilotTokenEndpoint(domain string) string {
	if copilotTokenURL != "" {
		return copilotTokenURL
	}
	return fmt.Sprintf("https://api.%s/copilot_internal/v2/token", domain)
}

func applyCopilotAvailableModels(c Credential) {
	if ids := extraStringSlice(c, "availableModelIds"); ids != nil {
		models.SetAvailableModelIDs("github-copilot", ids)
	}
}

func refreshCopilotAccess(ctx context.Context, githubToken, enterprise string) (Credential, error) {
	domain := "github.com"
	if enterprise != "" {
		domain = enterprise
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotTokenEndpoint(domain), nil)
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
	cred := Credential{
		Type:    TypeOAuth,
		Access:  data.Token,
		Refresh: githubToken,
		Expires: exp - 5*60*1000,
	}
	ids, err := fetchCopilotAvailableModelIDs(ctx, cred.Access, enterprise)
	if err == nil {
		cred = cred.withExtra("availableModelIds", ids)
		applyCopilotAvailableModels(cred)
	}
	return cred, nil
}

func fetchCopilotAvailableModelIDs(ctx context.Context, token, enterprise string) ([]string, error) {
	base := copilotModelsBaseURL
	if base == "" {
		base = copilotBaseURL(token, enterprise)
	}
	allowFallback := base == "https://api.individual.githubcopilot.com"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("user-agent", "GitHubCopilotChat/0.35.0")
	req.Header.Set("editor-version", "vscode/1.107.0")
	req.Header.Set("editor-plugin-version", "copilot-chat/0.35.0")
	req.Header.Set("copilot-integration-id", "vscode-chat")
	req.Header.Set("x-github-api-version", copilotAPIVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("copilot models failed (%d): %s", resp.StatusCode, b)
	}
	available, _, err := parseGitHubCopilotModelCatalog(b, allowFallback)
	return available, err
}

func parseGitHubCopilotModelCatalog(raw []byte, allowPolicyFallback bool) (available, policy []string, err error) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, nil, err
	}
	data, ok := top["data"].([]any)
	if !ok {
		return nil, nil, fmt.Errorf("invalid Copilot models response")
	}
	type accountModel struct {
		id, policyState string
		pickerEnabled   bool
	}
	var account []accountModel
	for _, rawItem := range data {
		item, _ := rawItem.(map[string]any)
		id, _ := item["id"].(string)
		if item == nil || id == "" {
			continue
		}
		caps, _ := item["capabilities"].(map[string]any)
		var supports map[string]any
		if caps != nil {
			supports, _ = caps["supports"].(map[string]any)
		}
		if supports != nil {
			if tc, ok := supports["tool_calls"].(bool); ok && !tc {
				continue
			}
		}
		picker, _ := item["model_picker_enabled"].(bool)
		pol, _ := item["policy"].(map[string]any)
		state := ""
		if pol != nil {
			state, _ = pol["state"].(string)
		}
		account = append(account, accountModel{id, state, picker})
	}
	for _, m := range account {
		if m.pickerEnabled && m.policyState != "disabled" {
			available = append(available, m.id)
		}
	}
	if len(available) == 0 && allowPolicyFallback {
		for _, m := range account {
			if m.policyState == "enabled" {
				available = append(available, m.id)
			}
		}
	}
	return available, policy, nil
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
