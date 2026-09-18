package tools

import (
	"os"
	"strings"
)

var defaultContainerEnv = map[string]bool{
	"PATH":                 true,
	"HOME":                 true,
	"TERM":                 true,
	"LANG":                 true,
	"USER":                 true,
	"AI_AGENT":             true,
	"PIGO_SESSION_ID":      true,
	"PIGO_PROVIDER":        true,
	"PIGO_MODEL":           true,
	"PIGO_REASONING_LEVEL": true,
	"PI_SESSION_ID":        true,
	"PI_PROVIDER":          true,
	"PI_MODEL":             true,
	"PI_REASONING_LEVEL":   true,
}

func allowedContainerEnvName(name string, extra []string) bool {
	if isSecretEnvName(name) {
		return false
	}
	if defaultContainerEnv[name] {
		return true
	}
	if strings.HasPrefix(name, "LC_") {
		return true
	}
	for _, n := range extra {
		if n == name && !isSecretEnvName(n) {
			return true
		}
	}
	return false
}

func isSecretEnvName(name string) bool {
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

func skipSessionFileEnv(name string) bool {
	u := strings.ToUpper(name)
	return strings.HasSuffix(u, "_SESSION_FILE")
}

// filterContainerEnv builds KEY=val pairs for a tool container.
// Secret names are dropped even if listed in extraAllow.
func filterContainerEnv(host []string, extraAllow []string, extra map[string]string) []string {
	out := map[string]string{}
	order := make([]string, 0)
	add := func(k, v string) {
		if k == "" || skipSessionFileEnv(k) || !allowedContainerEnvName(k, extraAllow) {
			return
		}
		if _, ok := out[k]; !ok {
			order = append(order, k)
		}
		out[k] = v
	}
	for _, kv := range host {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		add(k, v)
	}
	for k, v := range extra {
		add(k, v)
	}
	pairs := make([]string, 0, len(order))
	for _, k := range order {
		pairs = append(pairs, k+"="+out[k])
	}
	return pairs
}

func hostEnviron() []string {
	return os.Environ()
}
