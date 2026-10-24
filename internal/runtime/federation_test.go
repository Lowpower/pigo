package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/models"
)

const federationSSE = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
	"event: message_delta\n" +
	"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
	"event: message_stop\n" +
	"data: {\"type\":\"message_stop\"}\n\n"

func TestBoundStreamFederationUsesBearer(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("header.payload.signature"), 0o600); err != nil {
		t.Fatal(err)
	}
	authBlob := []byte("{\"openai\":{\"type\":\"api_key\",\"key\":\"sk-stored\"}}\n")
	authPath := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(authPath, authBlob, 0o600); err != nil {
		t.Fatal(err)
	}
	srv, hits := federationWireServer(t, http.StatusOK, "federated-token", 3600)
	setRuntimeFederationEnv(t, srv.URL, tokenPath, nil)

	fn := boundStream(dir, "anthropic", "")
	if fn == nil {
		t.Fatal("bound stream")
	}
	for i := 0; i < 2; i++ {
		stream, err := fn(context.Background(), ai.Context{
			Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
		}, ai.Options{Model: "claude-test"})
		if err != nil {
			t.Fatal(err)
		}
		for range stream.Events() {
		}
	}
	tokenHits := federationWireHits(hits, "/v1/oauth/token")
	if len(tokenHits) != 1 {
		t.Fatalf("token requests = %d", len(tokenHits))
	}
	msgHits := federationWireHits(hits, "/v1/messages")
	if len(msgHits) != 2 {
		t.Fatalf("message requests = %d", len(msgHits))
	}
	for _, hit := range msgHits {
		if hit.Authorization != "Bearer federated-token" {
			t.Fatalf("authorization = %q", hit.Authorization)
		}
		if hit.APIKey != "" {
			t.Fatalf("x-api-key = %q", hit.APIKey)
		}
		if strings.Contains(hit.Beta, "oidc-federation") || strings.Contains(hit.Beta, "oauth-2025-04-20") {
			t.Fatalf("beta = %q", hit.Beta)
		}
	}
	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, authBlob) {
		t.Fatalf("auth.json changed: %s", got)
	}
}

func TestBoundStreamFederationAPIKeySkipsExchange(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, hits := federationWireServer(t, http.StatusOK, "federated-token", 3600)
	setRuntimeFederationEnv(t, srv.URL, tokenPath, map[string]string{"ANTHROPIC_API_KEY": "sk-live"})
	pinAnthropicModelBase(t, "fed-api-key-test", srv.URL)

	fn := boundStream(dir, "anthropic", "")
	stream, err := fn(context.Background(), ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	}, ai.Options{Model: "fed-api-key-test"})
	if err != nil {
		t.Fatal(err)
	}
	for range stream.Events() {
	}
	if got := federationWireHits(hits, "/v1/oauth/token"); len(got) != 0 {
		t.Fatalf("token requests = %d", len(got))
	}
	msgHits := federationWireHits(hits, "/v1/messages")
	if len(msgHits) != 1 {
		t.Fatalf("message requests = %d", len(msgHits))
	}
	if msgHits[0].APIKey != "sk-live" {
		t.Fatalf("x-api-key = %q", msgHits[0].APIKey)
	}
	if msgHits[0].Authorization != "" {
		t.Fatalf("authorization = %q", msgHits[0].Authorization)
	}
}

func TestBoundStreamFederationExchangeError(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, hits := federationWireServer(t, http.StatusInternalServerError, "", 0)
	setRuntimeFederationEnv(t, srv.URL, tokenPath, nil)
	fn := boundStream(dir, "anthropic", "")
	_, err := fn(context.Background(), ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	}, ai.Options{Model: "claude-test"})
	if err == nil || !auth.IsFederationError(err) || !strings.Contains(err.Error(), "500") {
		t.Fatal(err)
	}
	if got := federationWireHits(hits, "/v1/messages"); len(got) != 0 {
		t.Fatalf("message requests = %d", len(got))
	}
}

type federationWireHit struct {
	Path          string
	Authorization string
	APIKey        string
	Beta          string
}

func federationWireServer(t *testing.T, tokenStatus int, token string, expires int) (*httptest.Server, *[]federationWireHit) {
	t.Helper()
	var mu sync.Mutex
	hits := &[]federationWireHit{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*hits = append(*hits, federationWireHit{
			Path:          r.URL.Path,
			Authorization: r.Header.Get("Authorization"),
			APIKey:        r.Header.Get("x-api-key"),
			Beta:          r.Header.Get("anthropic-beta"),
		})
		mu.Unlock()
		if r.URL.Path == "/v1/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tokenStatus)
			if tokenStatus >= 200 && tokenStatus < 300 {
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires_in": expires})
			}
			return
		}
		if r.URL.Path == "/v1/messages" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, federationSSE)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func federationWireHits(hits *[]federationWireHit, path string) []federationWireHit {
	var out []federationWireHit
	for _, hit := range *hits {
		if hit.Path == path {
			out = append(out, hit)
		}
	}
	return out
}

func setRuntimeFederationEnv(t *testing.T, base, tokenFile string, extra map[string]string) {
	t.Helper()
	auth.ClearProcessAPIKey("anthropic")
	t.Cleanup(func() { auth.ClearProcessAPIKey("anthropic") })
	for _, name := range []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_OAUTH_TOKEN",
		"ANTHROPIC_FEDERATION_RULE_ID",
		"ANTHROPIC_ORGANIZATION_ID",
		"ANTHROPIC_IDENTITY_TOKEN_FILE",
		"ANTHROPIC_SERVICE_ACCOUNT_ID",
		"ANTHROPIC_WORKSPACE_ID",
		"ANTHROPIC_BASE_URL",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("ANTHROPIC_BASE_URL", base)
	t.Setenv("ANTHROPIC_FEDERATION_RULE_ID", "fdrl_test")
	t.Setenv("ANTHROPIC_ORGANIZATION_ID", "org-test")
	t.Setenv("ANTHROPIC_IDENTITY_TOKEN_FILE", tokenFile)
	for k, v := range extra {
		t.Setenv(k, v)
	}
}

func pinAnthropicModelBase(t *testing.T, id, base string) {
	t.Helper()
	spec, ok := models.LookupProvider("anthropic")
	if !ok {
		t.Fatal("anthropic provider missing")
	}
	saved := spec
	saved.Models = append([]models.Model(nil), spec.Models...)
	spec.Models = append(append([]models.Model(nil), spec.Models...), models.Model{
		Provider: "anthropic",
		ID:       id,
		API:      "anthropic-messages",
		BaseURL:  base,
	})
	models.RegisterProvider(spec)
	t.Cleanup(func() { models.RegisterProvider(saved) })
}
