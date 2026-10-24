package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	federationSource         = "workload identity federation"
	federationGrantType      = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	federationBetaHeader     = "oauth-2025-04-20,oidc-federation-2026-04-01"
	federationTokenPath      = "/v1/oauth/token"
	federationAssertionLimit = 16 * 1024
	federationRefreshWindow  = 120 * time.Second

	envFederationRuleID      = "ANTHROPIC_FEDERATION_RULE_ID"
	envFederationOrgID       = "ANTHROPIC_ORGANIZATION_ID"
	envFederationTokenFile   = "ANTHROPIC_IDENTITY_TOKEN_FILE"
	envFederationServiceAcct = "ANTHROPIC_SERVICE_ACCOUNT_ID"
	envFederationWorkspaceID = "ANTHROPIC_WORKSPACE_ID"
	envAnthropicBaseURL      = "ANTHROPIC_BASE_URL"
	defaultAnthropicFedBase  = "https://api.anthropic.com"
)

// federationError is a workload-identity failure. boundStream returns it to the
// caller; other Resolve errors stay on the previous empty-key path.
type federationError struct{ err error }

func (e *federationError) Error() string {
	return "anthropic workload identity: " + e.err.Error()
}

func (e *federationError) Unwrap() error { return e.err }

// IsFederationError reports a workload-identity exchange failure.
func IsFederationError(err error) bool {
	var fe *federationError
	return errors.As(err, &fe)
}

func federationErr(err error) error {
	if err == nil {
		return nil
	}
	var fe *federationError
	if errors.As(err, &fe) {
		return err
	}
	return &federationError{err: err}
}

type federationConfig struct {
	baseURL        string
	fromEnv        bool
	ruleID         string
	orgID          string
	tokenFile      string
	serviceAccount string
	workspace      string
}

func anthropicFederationConfigured(providerID string) bool {
	if providerID != "anthropic" {
		return false
	}
	_, ok := anthropicFederationConfig()
	return ok
}

func anthropicFederationConfig() (federationConfig, bool) {
	cfg := federationConfig{
		ruleID:         strings.TrimSpace(os.Getenv(envFederationRuleID)),
		orgID:          strings.TrimSpace(os.Getenv(envFederationOrgID)),
		tokenFile:      strings.TrimSpace(os.Getenv(envFederationTokenFile)),
		serviceAccount: strings.TrimSpace(os.Getenv(envFederationServiceAcct)),
		workspace:      strings.TrimSpace(os.Getenv(envFederationWorkspaceID)),
	}
	if cfg.ruleID == "" || cfg.orgID == "" || cfg.tokenFile == "" {
		return federationConfig{}, false
	}
	base, fromEnv, err := federationBaseURL()
	if err != nil {
		// Still "configured" so Resolve reports the endpoint error instead of
		// falling through to the no-key mock. Exchange rejects the URL.
		cfg.baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv(envAnthropicBaseURL)), "/")
		cfg.fromEnv = os.Getenv(envAnthropicBaseURL) != ""
		return cfg, true
	}
	cfg.baseURL = base
	cfg.fromEnv = fromEnv
	return cfg, true
}

func federationBaseURL() (string, bool, error) {
	raw := strings.TrimSpace(os.Getenv(envAnthropicBaseURL))
	fromEnv := raw != ""
	if raw == "" {
		raw = defaultAnthropicFedBase
	}
	base := strings.TrimRight(raw, "/")
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fromEnv, errors.New("invalid federation token endpoint")
	}
	switch u.Scheme {
	case "https":
		return base, fromEnv, nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return base, fromEnv, nil
		}
	}
	return "", fromEnv, errors.New("federation token endpoint must use https or a loopback http URL")
}

func applyAnthropicFederation(ctx context.Context, res *Result) (*Result, error) {
	cfg, ok := anthropicFederationConfig()
	if !ok {
		return nil, federationErr(errors.New("workload identity is not configured"))
	}
	token, err := anthropicFederationToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	res.Auth.Headers = map[string]string{"Authorization": "Bearer " + token}
	if cfg.fromEnv {
		res.Auth.BaseURL = cfg.baseURL
	}
	return res, nil
}

type federationCacheEntry struct {
	token     string
	expiresAt time.Time
}

type federationFlight struct {
	done      chan struct{}
	token     string
	expiresAt time.Time
	err       error
}

var (
	federationMu      sync.Mutex
	federationCache   = map[string]federationCacheEntry{}
	federationFlights = map[string]*federationFlight{}
)

func federationCacheKey(cfg federationConfig) string {
	return strings.Join([]string{cfg.baseURL, cfg.ruleID, cfg.orgID, cfg.serviceAccount, cfg.workspace, cfg.tokenFile}, "\x00")
}

func anthropicFederationToken(ctx context.Context, cfg federationConfig) (string, error) {
	if _, _, err := federationBaseURL(); err != nil {
		return "", federationErr(err)
	}
	key := federationCacheKey(cfg)
	federationMu.Lock()
	if entry, ok := federationCache[key]; ok && time.Until(entry.expiresAt) > federationRefreshWindow {
		token := entry.token
		federationMu.Unlock()
		return token, nil
	}
	if flight, ok := federationFlights[key]; ok {
		federationMu.Unlock()
		select {
		case <-flight.done:
			if flight.err != nil {
				return "", flight.err
			}
			return flight.token, nil
		case <-ctx.Done():
			return "", federationErr(ctx.Err())
		}
	}
	flight := &federationFlight{done: make(chan struct{})}
	federationFlights[key] = flight
	federationMu.Unlock()

	token, expiresAt, err := exchangeFederation(ctx, cfg)
	federationMu.Lock()
	if err == nil {
		federationCache[key] = federationCacheEntry{token: token, expiresAt: expiresAt}
	}
	flight.token = token
	flight.expiresAt = expiresAt
	flight.err = err
	delete(federationFlights, key)
	close(flight.done)
	federationMu.Unlock()
	if err != nil {
		return "", err
	}
	return token, nil
}

func exchangeFederation(ctx context.Context, cfg federationConfig) (string, time.Time, error) {
	jwt, err := readIdentityToken(cfg.tokenFile)
	if err != nil {
		return "", time.Time{}, federationErr(err)
	}
	body := map[string]string{
		"grant_type":         federationGrantType,
		"assertion":          jwt,
		"federation_rule_id": cfg.ruleID,
		"organization_id":    cfg.orgID,
	}
	if cfg.serviceAccount != "" {
		body["service_account_id"] = cfg.serviceAccount
	}
	if cfg.workspace != "" {
		body["workspace_id"] = cfg.workspace
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", time.Time{}, federationErr(err)
	}
	rctx, cancel := context.WithTimeout(ctx, oauthRefreshTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, cfg.baseURL+federationTokenPath, bytes.NewReader(raw))
	if err != nil {
		return "", time.Time{}, federationErr(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", federationBetaHeader)
	client := &http.Client{
		Timeout: oauthRefreshTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("federation token endpoint must not redirect")
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, federationErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", time.Time{}, federationErr(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, federationErr(fmt.Errorf("token exchange failed with status %d", resp.StatusCode))
	}
	var parsed struct {
		AccessToken string   `json:"access_token"`
		ExpiresIn   *float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return "", time.Time{}, federationErr(fmt.Errorf("token endpoint response: %w", err))
	}
	if parsed.AccessToken == "" || parsed.ExpiresIn == nil || math.IsNaN(*parsed.ExpiresIn) || math.IsInf(*parsed.ExpiresIn, 0) || *parsed.ExpiresIn < 0 {
		return "", time.Time{}, federationErr(errors.New("token endpoint response missing access_token or expires_in"))
	}
	expiresAt := time.Now().Add(time.Duration(*parsed.ExpiresIn * float64(time.Second)))
	return parsed.AccessToken, expiresAt, nil
}

func readIdentityToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read identity token: %w", err)
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", errors.New("identity token is empty")
	}
	if len(token) > federationAssertionLimit {
		return "", errors.New("identity token exceeds the 16KiB assertion limit")
	}
	return token, nil
}
