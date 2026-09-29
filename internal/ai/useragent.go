package ai

import (
	"net/http"
	"runtime"
	"strings"

	"github.com/Lowpower/pigo/internal/version"
)

func formatProviderUserAgent(ver, goos, release, goarch string) string {
	release = strings.TrimSpace(release)
	if release == "" || strings.ContainsAny(release, "\r\n") {
		release = "unknown"
	}
	return "pigo/" + ver + " (" + goos + " " + release + "; " + goarch + ")"
}

func providerUserAgent() string {
	return formatProviderUserAgent(version.Version, runtime.GOOS, osRelease(), runtime.GOARCH)
}

func setDefaultUserAgent(h http.Header) {
	if h.Get("User-Agent") != "" {
		return
	}
	h.Set("User-Agent", providerUserAgent())
}

func lookupHeader(h map[string]string, name string) (string, bool) {
	for k, v := range h {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

// selectUserAgent resolves the User-Agent for clients that set their own
// before the request reaches us. A non-empty extra header wins, then a
// non-empty configured header, then the pigo default. An explicit empty extra
// header clears the value.
func selectUserAgent(configured, extra map[string]string) (string, bool) {
	if v, ok := lookupHeader(extra, "User-Agent"); ok {
		return v, v != ""
	}
	if v, ok := lookupHeader(configured, "User-Agent"); ok && v != "" {
		return v, true
	}
	return providerUserAgent(), true
}
