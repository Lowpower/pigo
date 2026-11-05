package oauth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/auth"
)

const (
	// FileName is the credential file inside the agent directory.
	FileName = "mcp-auth.json"
	// ClientMetadataURL is the OAuth client id for clientRegistration "cimd".
	// It is the permanent raw URL of docs/oauth/client.json and must not change.
	ClientMetadataURL = "https://raw.githubusercontent.com/Lowpower/pigo/main/docs/oauth/client.json"

	defaultClientName = "pigo"
	callbackPath      = "/callback"
	asTimeout         = 15 * time.Second
	refreshSkew       = 30 * time.Second
	protocolVersion   = "2025-11-25"
)

// ErrAuthorizationRequired means the user has to sign in before the call can proceed.
var ErrAuthorizationRequired = errors.New("mcp oauth authorization required")

// ErrSignInCancelled means the caller cancelled sign-in.
var ErrSignInCancelled = errors.New("sign-in canceled")

// IssuerError is a refused code exchange: the authorization response iss does
// not match the discovered issuer, or there is nothing to compare.
type IssuerError struct {
	Expected string
	Got      string
}

func (e *IssuerError) Error() string {
	return fmt.Sprintf("oauth issuer mismatch: got %q want %q", e.Got, e.Expected)
}

// ConfigScope is where an MCP server entry was defined.
type ConfigScope string

const (
	// ScopeUser is the user-level mcp.json.
	ScopeUser ConfigScope = "user"
	// ScopeExtension is a server registered by an extension.
	ScopeExtension ConfigScope = "extension"
	// ScopeProject is a project mcp.json. auth.provider is rejected there.
	ScopeProject ConfigScope = "project"
)

// Server is one remote MCP HTTP server.
type Server struct {
	Name string
	URL  string
}

// Settings is the oauth object of one HTTP server.
type Settings struct {
	ClientID              string
	ClientSecret          string
	CallbackPort          int
	Scope                 string
	ClientName            string
	ClientRegistration    string
	AuthServerMetadataURL string
}

// Prompt shows the authorization URL. Open replaces the default browser launch.
type Prompt struct {
	Notify func(auth.Event)
	Open   func(rawURL string)
}

// Challenge is a WWW-Authenticate bearer challenge from the resource server.
type Challenge struct {
	Error               string
	Scope               string
	ResourceMetadataURL string
}

// Request is one sign-in. Challenge is set for an insufficient_scope step-up.
type Request struct {
	Settings  Settings
	Prompt    Prompt
	Challenge Challenge
}

// Client performs MCP OAuth and attaches access tokens to HTTP calls.
type Client struct {
	Store *Store
	// HTTP is used for the authorization server and for Call. Nil uses http.DefaultClient.
	// Each authorization-server request still has its own 15s timeout.
	HTTP *http.Client
}

// Record is one server's persisted OAuth state.
type Record struct {
	ClientID                string  `json:"clientId,omitempty"`
	ClientSecret            string  `json:"clientSecret,omitempty"`
	TokenEndpointAuthMethod string  `json:"tokenEndpointAuthMethod,omitempty"`
	RedirectURI             string  `json:"redirectUri,omitempty"`
	AuthServerMetadataURL   string  `json:"authServerMetadataUrl,omitempty"`
	Resource                string  `json:"resource,omitempty"`
	Tokens                  *Tokens `json:"tokens,omitempty"`
	TokensExpireAt          *int64  `json:"tokensExpireAt,omitempty"`
	CodeVerifier            string  `json:"codeVerifier,omitempty"`
	OAuthState              string  `json:"oauthState,omitempty"`
}

// Tokens is an OAuth token response kept on disk.
type Tokens struct {
	AccessToken  string `json:"accessToken"`
	TokenType    string `json:"tokenType,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresIn    *int64 `json:"expiresIn,omitempty"`
}

func (s Settings) validate() error {
	switch s.ClientRegistration {
	case "", "dcr", "cimd":
	default:
		return fmt.Errorf("unknown oauth.clientRegistration %q", s.ClientRegistration)
	}
	if s.ClientRegistration == "cimd" && (s.ClientID != "" || s.ClientName != "") {
		return errors.New(`oauth.clientRegistration "cimd" cannot be combined with clientId or clientName`)
	}
	if s.CallbackPort < 0 || s.CallbackPort > 65535 {
		return errors.New("oauth.callbackPort is invalid")
	}
	if s.AuthServerMetadataURL != "" {
		if _, err := secureEndpoint(s.AuthServerMetadataURL); err != nil {
			return fmt.Errorf("oauth.authServerMetadataUrl: %w", err)
		}
	}
	return nil
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) storeOrErr() (*Store, error) {
	if c == nil || c.Store == nil {
		return nil, errors.New("mcp oauth store is nil")
	}
	return c.Store, nil
}

func parseServerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid mcp server url %q", raw)
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u, nil
}

func matchResponseIssuer(issuer, iss string) error {
	if issuer == "" || iss == "" || !sameIssuer(issuer, iss) {
		return &IssuerError{Expected: issuer, Got: iss}
	}
	return nil
}

func sameIssuer(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

func stepUpScope(granted, challenged string) string {
	if strings.TrimSpace(challenged) == "" {
		return ""
	}
	return mergeScopes(granted, challenged)
}

func mergeScopes(parts ...string) string {
	seen := map[string]struct{}{}
	var out []string
	for _, part := range parts {
		for _, s := range strings.Fields(part) {
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return strings.Join(out, " ")
}

func requestedScope(settings Settings, granted string, ch Challenge, resourceScopes []string) string {
	if ch.Error == "insufficient_scope" {
		if merged := mergeScopes(settings.Scope, stepUpScope(granted, ch.Scope)); merged != "" {
			return merged
		}
	}
	if settings.Scope != "" {
		return settings.Scope
	}
	if ch.Scope != "" && ch.Error != "insufficient_scope" {
		return ch.Scope
	}
	if len(resourceScopes) > 0 {
		return strings.Join(resourceScopes, " ")
	}
	return ""
}

func needsRefresh(rec Record, now time.Time) bool {
	if rec.Tokens == nil || rec.Tokens.RefreshToken == "" || rec.TokensExpireAt == nil {
		return false
	}
	return *rec.TokensExpireAt <= now.Add(refreshSkew).UnixMilli()
}

func expireAt(seconds *int64, now time.Time) *int64 {
	if seconds == nil {
		return nil
	}
	ms := now.Add(time.Duration(*seconds) * time.Second).UnixMilli()
	return &ms
}
