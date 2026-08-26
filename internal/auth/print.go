package auth

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultBearerMinExpiry is print-bearer-token's default --min-expiry (30m).
const DefaultBearerMinExpiry = 30 * time.Minute

var minExpiryRe = regexp.MustCompile(`^(\d+)(ms|s|m|h)$`)

// ParseMinExpiry parses print-bearer-token --min-expiry (30m, 1h, …).
func ParseMinExpiry(s string) (time.Duration, error) {
	m := minExpiryRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0, fmt.Errorf("--min-expiry must use a duration such as 30m or 1h")
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "ms":
		return time.Duration(n) * time.Millisecond, nil
	case "s":
		return time.Duration(n) * time.Second, nil
	case "m":
		return time.Duration(n) * time.Minute, nil
	default:
		return time.Duration(n) * time.Hour, nil
	}
}

// PrintSecret prints an api_key or oauth bearer for a provider.
func PrintSecret(ctx context.Context, agentDir, provider, kind string, minExpiry time.Duration) (string, error) {
	p, ok := Lookup(provider)
	if !ok {
		return "", fmt.Errorf("unknown provider %q", provider)
	}
	s := Open(agentDir)
	infos, err := s.List()
	if err != nil {
		return "", err
	}
	var typ string
	for _, i := range infos {
		if i.ProviderID == provider {
			typ = i.Type
		}
	}
	if kind == TypeAPIKey && typ == TypeOAuth {
		return "", fmt.Errorf("provider %q is configured with OAuth, not an API key", provider)
	}
	if kind == TypeOAuth && typ != TypeOAuth {
		return "", fmt.Errorf("provider %q is not configured with an OAuth bearer token", provider)
	}
	opts := ResolveOpts{}
	if kind == TypeOAuth {
		if minExpiry <= 0 {
			minExpiry = DefaultBearerMinExpiry
		}
		opts.MinOAuthValidityMs = minExpiry.Milliseconds()
	}
	res, err := Resolve(ctx, s, p, opts)
	if err != nil {
		return "", err
	}
	val := Secret(res)
	if val == "" {
		if kind == TypeOAuth {
			return "", fmt.Errorf("no usable OAuth bearer token is configured")
		}
		return "", fmt.Errorf("no usable API key is configured")
	}
	return val, nil
}

// CheckResult is the auth check payload.
type CheckResult struct {
	Status      string `json:"status"`
	Provider    string `json:"provider"`
	Reason      string `json:"reason,omitempty"`
	AuthType    string `json:"authType,omitempty"`
	Credentials string `json:"credentials,omitempty"`
}

// CheckProvider runs pi auth check.
func CheckProvider(ctx context.Context, agentDir, provider string, refresh, includeCreds bool) CheckResult {
	if _, ok := Lookup(provider); !ok {
		return CheckResult{Status: "not_ready", Provider: provider, Reason: "provider_not_found"}
	}
	var s *Store
	if refresh {
		s = Open(agentDir)
	} else {
		s = ReadOnly(agentDir)
	}
	chk := CheckAuth(s, provider)
	if chk == nil {
		return CheckResult{Status: "not_ready", Provider: provider, Reason: "credentials_not_configured"}
	}
	if refresh {
		p, _ := Lookup(provider)
		res, err := Resolve(ctx, s, p, ResolveOpts{})
		if err != nil {
			return CheckResult{Status: "invalid", Provider: provider, Reason: "invalid_state"}
		}
		if res == nil {
			return CheckResult{Status: "not_ready", Provider: provider, Reason: "credentials_not_configured"}
		}
		out := CheckResult{Status: "ready", Provider: provider, AuthType: chk.Type}
		if includeCreds {
			out.Credentials = Secret(res)
		}
		return out
	}
	out := CheckResult{Status: "ready", Provider: provider, AuthType: chk.Type}
	if includeCreds {
		c, ok, _ := s.Read(provider)
		if ok && c.Type == TypeOAuth {
			out.Credentials = c.Access
		} else if ok {
			out.Credentials = c.Key
		}
	}
	return out
}
