package auth

import (
	"os"
	"strings"
)

// IsAnthropicSubscriptionAuth reports whether Anthropic credentials look like
// Claude subscription OAuth (sk-ant-oat…) rather than a pay-as-you-go API key.
func IsAnthropicSubscriptionAuth(agentDir string) bool {
	s := Open(agentDir)
	chk := CheckAuth(s, "anthropic")
	if chk == nil {
		return oatInEnv()
	}
	if chk.Type == TypeOAuth {
		return true
	}
	c, found, err := s.Read("anthropic")
	if err == nil && found && (isAnthropicOAT(c.Key) || isAnthropicOAT(c.Access)) {
		return true
	}
	return oatInEnv()
}

func oatInEnv() bool {
	for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_API_KEY"} {
		if isAnthropicOAT(os.Getenv(name)) {
			return true
		}
	}
	return false
}

func isAnthropicOAT(v string) bool {
	return strings.HasPrefix(strings.TrimSpace(v), "sk-ant-oat")
}
