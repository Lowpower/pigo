package bugreport

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/Lowpower/pigo/internal/session"
)

// Redacted is the replacement for credential-shaped settings values.
const Redacted = "<redacted>"

var (
	sensitiveKey = regexp.MustCompile(`(?i)(?:^|[-_])(api[-_]?key|secret|token|password|passwd|credential|authorization|cookie)(?:$|[-_])`)
	nestedURL    = regexp.MustCompile(`(?i)^([a-z][a-z0-9+.-]*:)([a-z][a-z0-9+.-]*://.*)$`)
)

func isSensitiveKey(key string) bool {
	if key == "" || key == "trackingId" {
		return false
	}
	var b strings.Builder
	for i, r := range key {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := rune(key[i-1])
			if prev != '_' && prev != '-' {
				b.WriteByte('_')
			}
		}
		b.WriteRune(r)
	}
	return sensitiveKey.MatchString(b.String())
}

func redactJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			if k == "trackingId" {
				continue
			}
			if isSensitiveKey(k) {
				out[k] = Redacted
				continue
			}
			out[k] = redactJSON(child)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = redactJSON(child)
		}
		return out
	case string:
		return redactString(t)
	default:
		return v
	}
}

func redactString(s string) string {
	s = redactURL(s)
	return string(session.RedactSecrets([]byte(s)))
}

func redactURL(value string) string {
	if m := nestedURL.FindStringSubmatch(value); m != nil {
		return m[1] + redactURL(m[2])
	}
	if !strings.Contains(value, "://") {
		return value
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return value
	}
	changed := false
	if u.User != nil {
		u.User = nil
		changed = true
	}
	q := u.Query()
	for _, k := range queryKeys(q) {
		if isSensitiveKey(k) {
			q.Set(k, Redacted)
			changed = true
		}
	}
	if !changed {
		return value
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func queryKeys(q url.Values) []string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	return keys
}
