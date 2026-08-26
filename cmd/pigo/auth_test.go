package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/auth"
)

func TestAuthPrintAndCheckCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", dir)
	s := auth.Open(dir)
	_, err := s.Modify("anthropic", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: auth.TypeAPIKey, Key: "sk-test"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	root := newRootCmd()
	root.SetArgs([]string{"auth", "print-api-key", "--provider", "anthropic"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if strings.TrimSpace(out.String()) != "sk-test" {
		t.Fatalf("print-api-key: %q", out.String())
	}

	root = newRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"auth", "print-bearer-token", "--provider", "anthropic"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected bearer error for api_key provider")
	}

	_, _ = s.Modify("openai-codex", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{
			Type:    auth.TypeOAuth,
			Access:  "oauth-access",
			Refresh: "oauth-refresh",
			Expires: time.Now().Add(2 * time.Hour).UnixMilli(),
		}, nil
	})
	root = newRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"auth", "print-bearer-token", "--provider", "openai-codex", "--min-expiry", "30m"})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if strings.TrimSpace(out.String()) != "oauth-access" {
		t.Fatalf("bearer: %q", out.String())
	}

	root = newRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"auth", "check", "--provider", "openai-codex", "--no-refresh", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if !strings.Contains(out.String(), `"authType":"oauth"`) {
		t.Fatalf("check json: %s", out.String())
	}
	_ = os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o600)
}
