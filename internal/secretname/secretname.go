// Package secretname classifies credential-bearing environment variable names.
package secretname

import "strings"

// IsEnv reports whether name looks like an API key, token, password, or OAuth secret.
func IsEnv(name string) bool {
	u := strings.ToUpper(strings.TrimSpace(name))
	if u == "" {
		return false
	}
	if u == "AUTHORIZATION" {
		return true
	}
	if strings.HasSuffix(u, "_API_KEY") || strings.HasSuffix(u, "_TOKEN") ||
		strings.HasSuffix(u, "_SECRET") || strings.HasSuffix(u, "_PASSWORD") {
		return true
	}
	if strings.Contains(u, "OAUTH") {
		return true
	}
	if strings.Contains(u, "REFRESH") && strings.Contains(u, "TOKEN") {
		return true
	}
	return false
}

// ShouldDrop reports whether a process should omit this env var when spawning
// untrusted children (extensions, tool containers).
func ShouldDrop(name string) bool {
	if IsEnv(name) {
		return true
	}
	return strings.HasSuffix(strings.ToUpper(name), "_SESSION_FILE")
}

// FilterEnviron removes credential-bearing KEY=val pairs from an environ list.
func FilterEnviron(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || ShouldDrop(k) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
