package oauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClientMetadataDocumentFile(t *testing.T) {
	root := moduleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "oauth", "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ClientID                string   `json:"client_id"`
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ClientID != ClientMetadataURL {
		t.Fatalf("client_id = %s", doc.ClientID)
	}
	if doc.ClientName != "pigo" {
		t.Fatalf("client_name = %s", doc.ClientName)
	}
	if len(doc.RedirectURIs) != 1 || doc.RedirectURIs[0] != "http://127.0.0.1/callback" {
		t.Fatalf("redirect_uris = %#v", doc.RedirectURIs)
	}
	if doc.TokenEndpointAuthMethod != "none" {
		t.Fatalf("auth method = %s", doc.TokenEndpointAuthMethod)
	}
	if !has(doc.GrantTypes, "authorization_code") || !has(doc.GrantTypes, "refresh_token") {
		t.Fatalf("grant_types = %#v", doc.GrantTypes)
	}
	if !has(doc.ResponseTypes, "code") {
		t.Fatalf("response_types = %#v", doc.ResponseTypes)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
