package sandbox

import (
	"net"
	"strconv"
	"strings"
)

// HostAllowed reports whether host[:port] may be dialed.
// Denied patterns win; an empty allow list denies everything.
func HostAllowed(hostport string, allowed, denied []string) bool {
	host, port := splitHostPort(hostport)
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	for _, d := range denied {
		if matchDomain(host, port, d) {
			return false
		}
	}
	if len(allowed) == 0 {
		return false
	}
	for _, a := range allowed {
		if matchDomain(host, port, a) {
			return true
		}
	}
	return false
}

func splitHostPort(hostport string) (host, port string) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", ""
	}
	if strings.HasPrefix(hostport, "[") {
		if h, p, err := net.SplitHostPort(hostport); err == nil {
			return h, p
		}
		return strings.Trim(hostport, "[]"), ""
	}
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		return h, p
	}
	return hostport, ""
}

func matchDomain(host, port, pattern string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	patHost, patPort := pattern, ""
	if i := strings.LastIndex(pattern, ":"); i >= 0 && !strings.Contains(pattern[i+1:], ":") {
		if _, err := strconv.Atoi(pattern[i+1:]); err == nil {
			patHost, patPort = pattern[:i], pattern[i+1:]
		}
	}
	if patPort != "" && port != "" && patPort != port {
		return false
	}
	if strings.HasPrefix(patHost, "*.") {
		suf := patHost[1:] // .example.com
		return strings.HasSuffix(host, suf) && len(host) > len(suf)
	}
	return host == patHost
}
