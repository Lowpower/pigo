package session

import (
	"os"
	"regexp"
)

var (
	shareSkRe     = regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`)
	shareGitHubRe = regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}`)
	shareBearerRe = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-+=/]{8,}`)
	shareKeyJSON  = regexp.MustCompile(`(?i)("(?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret|authorization)"\s*:\s*")([^"]+)(")`)
)

func redactSecrets(b []byte) []byte {
	s := string(b)
	s = shareKeyJSON.ReplaceAllString(s, `${1}[redacted]$3`)
	s = shareBearerRe.ReplaceAllString(s, "Bearer [redacted]")
	s = shareGitHubRe.ReplaceAllString(s, "[redacted]")
	s = shareSkRe.ReplaceAllString(s, "[redacted]")
	return []byte(s)
}

func redactShareFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, redactSecrets(b), 0o600)
}
