package sesssrv

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseUnixPath accepts unix:///abs/path.sock and returns the socket path.
func ParseUnixPath(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("unix listen/connect address is required")
	}
	u, err := url.Parse(addr)
	if err != nil {
		return "", fmt.Errorf("invalid address %q: %w", addr, err)
	}
	if u.Scheme != "unix" {
		return "", fmt.Errorf("unsupported transport %q (want unix)", u.Scheme)
	}
	path := u.Path
	if path == "" || path == "/" {
		return "", fmt.Errorf("unix address missing path")
	}
	return path, nil
}
