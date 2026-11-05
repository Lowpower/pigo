package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type asMeta struct {
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	ResponseTypes         []string
	AuthMethods           []string
	CodeChallenges        []string
	CIMD                  bool
}

type protected struct {
	Resource string
	Servers  []string
	Scopes   []string
}

type clientCreds struct {
	id     string
	secret string
	method string
}

func (c *Client) getJSON(ctx context.Context, rawURL string) (int, []byte, error) {
	endpoint, err := secureEndpoint(rawURL)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	return c.do(ctx, req)
}

func (c *Client) postForm(ctx context.Context, rawURL string, form url.Values, header http.Header) (int, []byte, error) {
	endpoint, err := secureEndpoint(rawURL)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		req.Header[k] = append([]string(nil), v...)
	}
	return c.do(ctx, req)
}

func (c *Client) postJSON(ctx context.Context, rawURL string, payload any) (int, []byte, error) {
	endpoint, err := secureEndpoint(rawURL)
	if err != nil {
		return 0, nil, err
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(b)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return c.do(ctx, req)
}

func (c *Client) do(ctx context.Context, req *http.Request) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, asTimeout)
	defer cancel()
	res, err := c.httpClient().Do(req.WithContext(ctx))
	if err != nil {
		return 0, nil, err
	}
	body, err := drain(res)
	if err != nil {
		return 0, nil, err
	}
	return res.StatusCode, body, nil
}

func drain(res *http.Response) ([]byte, error) {
	defer func() { _ = res.Body.Close() }()
	return io.ReadAll(io.LimitReader(res.Body, 1<<20))
}

func (c *Client) registerClient(ctx context.Context, d discovery, redirect, name, scope string) (clientCreds, error) {
	if d.RegistrationEndpoint == "" {
		return clientCreds{}, errors.New("authorization server does not support dynamic client registration")
	}
	if name == "" {
		name = defaultClientName
	}
	payload := map[string]any{
		"client_name":                name,
		"redirect_uris":              []string{redirect},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"application_type":           "native",
	}
	if scope != "" {
		payload["scope"] = scope
	}
	status, body, err := c.postJSON(ctx, d.RegistrationEndpoint, payload)
	if err != nil {
		return clientCreds{}, err
	}
	id, secret, err := parseRegistration(status, body)
	if err != nil {
		return clientCreds{}, err
	}
	return clientCreds{id: id, secret: secret, method: selectMethod(d.AuthMethods, secret)}, nil
}

func (c *Client) exchangeCode(ctx context.Context, d discovery, client clientCreds, redirect, verifier, code, scope, resource string) (Tokens, *int64, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirect)
	if resource != "" {
		form.Set("resource", resource)
	}
	return c.tokenRequest(ctx, d.TokenEndpoint, form, client, scope, "")
}

func (c *Client) exchangeRefresh(ctx context.Context, serverURL string, cur *Record) (Tokens, *int64, error) {
	d, err := c.discover(ctx, serverURL, cur.AuthServerMetadataURL, "")
	if err != nil {
		return Tokens{}, nil, err
	}
	resource := d.Resource
	if resource == "" {
		resource = cur.Resource
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cur.Tokens.RefreshToken)
	if resource != "" {
		form.Set("resource", resource)
	}
	client := clientCreds{id: cur.ClientID, secret: cur.ClientSecret, method: cur.TokenEndpointAuthMethod}
	if client.method == "" {
		client.method = selectMethod(d.AuthMethods, client.secret)
	}
	return c.tokenRequest(ctx, d.TokenEndpoint, form, client, "", cur.Tokens.Scope)
}

func (c *Client) tokenRequest(ctx context.Context, endpoint string, form url.Values, client clientCreds, fillScope, keepRefreshScope string) (Tokens, *int64, error) {
	header := make(http.Header)
	if err := applyAuth(client.method, client.id, client.secret, header, form); err != nil {
		return Tokens{}, nil, err
	}
	status, body, err := c.postForm(ctx, endpoint, form, header)
	if err != nil {
		return Tokens{}, nil, err
	}
	tokens, err := parseTokenResponse(status, body)
	if err != nil {
		return Tokens{}, nil, err
	}
	if tokens.Scope == "" {
		if fillScope != "" {
			tokens.Scope = fillScope
		} else if keepRefreshScope != "" {
			tokens.Scope = keepRefreshScope
		}
	}
	if form.Get("grant_type") == "refresh_token" && tokens.RefreshToken == "" {
		tokens.RefreshToken = form.Get("refresh_token")
	}
	return tokens, expireAt(tokens.ExpiresIn, time.Now()), nil
}

func applyAuth(method, id, secret string, header http.Header, form url.Values) error {
	switch method {
	case "client_secret_basic":
		if secret == "" {
			return errors.New("client_secret_basic requires a client secret")
		}
		header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(id+":"+secret)))
	case "client_secret_post":
		form.Set("client_id", id)
		if secret != "" {
			form.Set("client_secret", secret)
		}
	default:
		form.Set("client_id", id)
	}
	return nil
}

func selectMethod(supported []string, secret string) string {
	if len(supported) == 0 {
		if secret != "" {
			return "client_secret_basic"
		}
		return "none"
	}
	if secret != "" && contains(supported, "client_secret_basic") {
		return "client_secret_basic"
	}
	if secret != "" && contains(supported, "client_secret_post") {
		return "client_secret_post"
	}
	if contains(supported, "none") || secret == "" {
		return "none"
	}
	return "client_secret_post"
}

func parseRegistration(status int, body []byte) (string, string, error) {
	if status < 200 || status >= 300 {
		return "", "", fmt.Errorf("oauth registration HTTP %d", status)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", "", errors.New("invalid registration response")
	}
	id, ok := raw["client_id"].(string)
	if !ok || id == "" {
		return "", "", errors.New("invalid client_id")
	}
	secret, err := optionalStringField("client_secret", raw["client_secret"])
	if err != nil {
		return "", "", err
	}
	return id, secret, nil
}

func parseTokenResponse(status int, body []byte) (Tokens, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		if status < 200 || status >= 300 {
			return Tokens{}, fmt.Errorf("oauth token endpoint HTTP %d", status)
		}
		return Tokens{}, errors.New("invalid token response")
	}
	if e, ok := raw["error"].(string); ok && e != "" {
		desc := e
		if s, ok := raw["error_description"].(string); ok && s != "" {
			desc = s
		}
		return Tokens{}, fmt.Errorf("oauth error: %s", desc)
	}
	if status < 200 || status >= 300 {
		return Tokens{}, fmt.Errorf("oauth token endpoint HTTP %d", status)
	}
	access, ok := raw["access_token"].(string)
	if !ok || access == "" {
		return Tokens{}, errors.New("invalid access_token")
	}
	tokenType, err := optionalStringField("token_type", raw["token_type"])
	if err != nil {
		return Tokens{}, err
	}
	refresh, err := optionalStringField("refresh_token", raw["refresh_token"])
	if err != nil {
		return Tokens{}, err
	}
	scope, err := optionalStringField("scope", raw["scope"])
	if err != nil {
		return Tokens{}, err
	}
	expires, err := optionalInt(raw["expires_in"])
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{
		AccessToken:  access,
		TokenType:    tokenType,
		RefreshToken: refresh,
		Scope:        scope,
		ExpiresIn:    expires,
	}, nil
}

func parseAS(body []byte) (asMeta, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return asMeta{}, errors.New("invalid authorization server metadata")
	}
	issuer, err := requiredHTTPURL("authorization server issuer", raw["issuer"])
	if err != nil {
		return asMeta{}, err
	}
	authEndpoint, err := requiredHTTPURL("authorization endpoint", raw["authorization_endpoint"])
	if err != nil {
		return asMeta{}, err
	}
	tokenEndpoint, err := requiredHTTPURL("token endpoint", raw["token_endpoint"])
	if err != nil {
		return asMeta{}, err
	}
	reg, err := optionalHTTPURL("registration endpoint", raw["registration_endpoint"])
	if err != nil {
		return asMeta{}, err
	}
	responses, err := optionalStrings("response_types_supported", raw["response_types_supported"])
	if err != nil {
		return asMeta{}, err
	}
	if len(responses) == 0 {
		return asMeta{}, errors.New("invalid response_types_supported")
	}
	methods, err := optionalStrings("token_endpoint_auth_methods_supported", raw["token_endpoint_auth_methods_supported"])
	if err != nil {
		return asMeta{}, err
	}
	challenges, err := optionalStrings("code_challenge_methods_supported", raw["code_challenge_methods_supported"])
	if err != nil {
		return asMeta{}, err
	}
	return asMeta{
		Issuer:                issuer,
		AuthorizationEndpoint: authEndpoint,
		TokenEndpoint:         tokenEndpoint,
		RegistrationEndpoint:  reg,
		ResponseTypes:         responses,
		AuthMethods:           methods,
		CodeChallenges:        challenges,
		CIMD:                  optionalBool(raw["client_id_metadata_document_supported"]),
	}, nil
}

func parsePRM(body []byte) (*protected, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, errors.New("invalid OAuth protected resource metadata")
	}
	resource, err := requiredHTTPURL("protected resource", raw["resource"])
	if err != nil {
		return nil, err
	}
	servers, err := optionalStrings("authorization_servers", raw["authorization_servers"])
	if err != nil {
		return nil, err
	}
	for i, s := range servers {
		servers[i], err = httpURL(s, "authorization server URL")
		if err != nil {
			return nil, err
		}
	}
	scopes, err := optionalStrings("scopes_supported", raw["scopes_supported"])
	if err != nil {
		return nil, err
	}
	return &protected{Resource: resource, Servers: servers, Scopes: scopes}, nil
}

func optionalStringField(name string, v any) (string, error) {
	if v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("invalid %s", name)
	}
	return s, nil
}

func optionalInt(v any) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return nil, nil
		}
		return nil, errors.New("invalid expires_in")
	}
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, errors.New("invalid expires_in")
	}
	i := int64(n)
	return &i, nil
}

func optionalStrings(name string, v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid %s", name)
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("invalid %s", name)
		}
		out = append(out, s)
	}
	return out, nil
}

func optionalBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func requiredHTTPURL(name string, v any) (string, error) {
	s, err := optionalStringField(name, v)
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", fmt.Errorf("authorization server metadata has no %s", name)
	}
	return httpURL(s, name)
}

func optionalHTTPURL(name string, v any) (string, error) {
	s, err := optionalStringField(name, v)
	if err != nil || s == "" {
		return "", err
	}
	return httpURL(s, name)
}

func httpURL(raw, name string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid %s", name)
	}
	return u.String(), nil
}

func secureEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid oauth endpoint %q", raw)
	}
	if u.Scheme == "https" || (u.Scheme == "http" && loopback(u.Hostname())) {
		return u.String(), nil
	}
	return "", fmt.Errorf("oauth endpoint must use https: %s", u.Host)
}
