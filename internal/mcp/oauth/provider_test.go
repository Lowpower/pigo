package oauth

import (
	"context"
	"errors"
	"testing"
)

func TestValidateProviderAuth(t *testing.T) {
	cases := []struct {
		scope ConfigScope
		url   string
		id    string
		ok    bool
	}{
		{scope: ScopeProject, url: "https://example.com/mcp", id: "anthropic", ok: false},
		{scope: ScopeUser, url: "http://example.com/mcp", id: "anthropic", ok: false},
		{scope: ScopeUser, url: "http://127.0.0.1:9/mcp", id: "anthropic", ok: true},
		{scope: ScopeUser, url: "http://localhost/mcp", id: "openai", ok: true},
		{scope: ScopeUser, url: "http://[::1]:9/mcp", id: "openai", ok: true},
		{scope: ScopeExtension, url: "https://mcp.example/mcp", id: "openai", ok: true},
		{scope: ScopeUser, url: "https://example.com/mcp", id: "", ok: false},
	}
	for _, tc := range cases {
		err := ValidateProviderAuth(tc.scope, tc.url, tc.id)
		if tc.ok && err != nil {
			t.Errorf("ValidateProviderAuth(%q, %q, %q) = %v", tc.scope, tc.url, tc.id, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("ValidateProviderAuth(%q, %q, %q) succeeded", tc.scope, tc.url, tc.id)
		}
	}
}

func TestProviderBearerRereads(t *testing.T) {
	n := 0
	lookup := func(context.Context) (string, error) {
		n++
		if n == 1 {
			return "tok-1", nil
		}
		return "tok-2", nil
	}
	first, err := ProviderBearer(context.Background(), lookup)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProviderBearer(context.Background(), lookup)
	if err != nil {
		t.Fatal(err)
	}
	if first != "tok-1" || second != "tok-2" || n != 2 {
		t.Fatalf("tokens %q %q reads %d", first, second, n)
	}
	if _, err := ProviderBearer(context.Background(), func(context.Context) (string, error) {
		return "  ", nil
	}); err == nil {
		t.Fatal("empty token succeeded")
	}
}

func TestAuthServerMetadataURLRejectsPlainHTTP(t *testing.T) {
	c := &Client{Store: Open(t.TempDir())}
	err := c.SignIn(context.Background(), Server{Name: "docs", URL: "https://mcp.example/mcp"}, Request{
		Settings: Settings{AuthServerMetadataURL: "http://example.com/oauth-metadata"},
	})
	if err == nil {
		t.Fatal("plain http metadata URL succeeded")
	}
	if errors.Is(err, ErrSignInCancelled) {
		t.Fatal(err)
	}
}
