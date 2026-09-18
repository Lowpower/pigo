package eval

import (
	"github.com/Lowpower/pigo/internal/auth"
)

// HasCredential reports whether provider can be used without prompting.
// Empty agentDir still sees process environment keys.
func HasCredential(agentDir, provider string) bool {
	if provider == "" {
		provider = "anthropic"
	}
	return auth.CheckAuth(auth.Open(agentDir), provider) != nil
}
