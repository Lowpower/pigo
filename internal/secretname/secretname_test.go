package secretname

import (
	"strings"
	"testing"
)

func TestIsEnv(t *testing.T) {
	for _, name := range []string{"ANTHROPIC_API_KEY", "MY_TOKEN", "DB_PASSWORD", "OAUTH_ACCESS", "AUTHORIZATION"} {
		if !IsEnv(name) {
			t.Fatalf("expected secret %q", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "CLOUDFLARE_ACCOUNT_ID", "PIGO_EXT_HELPER"} {
		if IsEnv(name) {
			t.Fatalf("false positive %q", name)
		}
	}
}

func TestFilterEnvironDropsSecrets(t *testing.T) {
	got := FilterEnviron([]string{
		"PATH=/bin",
		"ANTHROPIC_API_KEY=sk",
		"PIGO_SESSION_FILE=/tmp/s.jsonl",
		"PIGO_EXT_HELPER=caps",
	})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "ANTHROPIC_API_KEY") || strings.Contains(joined, "PIGO_SESSION_FILE") {
		t.Fatalf("leaked: %s", joined)
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "PIGO_EXT_HELPER=caps") {
		t.Fatalf("dropped safe vars: %s", joined)
	}
}
