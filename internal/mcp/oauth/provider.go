package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var errNilRequest = errors.New("nil request")

// ValidateProviderAuth reports whether auth.provider may be used for this server.
// Project configuration cannot select a provider. Off-loopback HTTP URLs are rejected.
func ValidateProviderAuth(scope ConfigScope, serverURL, providerID string) error {
	if strings.TrimSpace(providerID) == "" {
		return errors.New("auth.provider is empty")
	}
	switch scope {
	case ScopeUser, ScopeExtension:
	case ScopeProject:
		return errors.New("auth.provider is only allowed in the user configuration and extension registration")
	default:
		return fmt.Errorf("unknown mcp config scope %q", scope)
	}
	u, err := url.Parse(serverURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid mcp server url %q", serverURL)
	}
	if u.Scheme != "https" && !loopback(u.Hostname()) {
		return errors.New("auth.provider requires https except on loopback")
	}
	return nil
}

// ProviderBearer reads a /login provider token. lookup is called on every request
// and must re-read storage; this function does not cache the result.
func ProviderBearer(ctx context.Context, lookup func(context.Context) (string, error)) (string, error) {
	if lookup == nil {
		return "", errors.New("missing provider credential lookup")
	}
	token, err := lookup(ctx)
	if err != nil {
		return "", err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("provider credential has no token")
	}
	return token, nil
}

// ParseWWWAuthenticate reads a bearer or DPoP challenge. Empty quoted values are absent.
func ParseWWWAuthenticate(header string) Challenge {
	header = strings.TrimSpace(header)
	if header == "" {
		return Challenge{}
	}
	fields := strings.Fields(header)
	scheme := strings.ToLower(fields[0])
	if scheme != "bearer" && scheme != "dpop" {
		return Challenge{}
	}
	return Challenge{
		Error:               wwwField(header, "error"),
		Scope:               wwwField(header, "scope"),
		ResourceMetadataURL: wwwField(header, "resource_metadata"),
	}
}

func wwwField(header, name string) string {
	re := regexp.MustCompile(`(?i)(?:^|[,\s])` + regexp.QuoteMeta(name) + `=(?:"([^"]*)"|([^\s,]+))`)
	m := re.FindStringSubmatch(header)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}
