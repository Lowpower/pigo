package auth

import (
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestOpenRouterCatalogKeyUsesRequestAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "sk-env")
	if got := models.OpenRouterKey(dir); got != "sk-env" {
		t.Fatalf("env = %q", got)
	}

	if _, err := Open(dir).Modify("openrouter", func(*Credential) (*Credential, error) {
		return &Credential{Type: TypeOAuth, Access: "sk-oauth"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := models.OpenRouterKey(dir); got != "sk-oauth" {
		t.Fatalf("oauth = %q", got)
	}

	SetProcessAPIKey("openrouter", "sk-proc")
	t.Cleanup(func() { ClearProcessAPIKey("openrouter") })
	if got := models.OpenRouterKey(dir); got != "sk-proc" {
		t.Fatalf("process = %q", got)
	}
}
