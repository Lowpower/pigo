package auth

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
)

func TestFederationExchangeAndCache(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("header.payload.signature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	authBlob := []byte("{\"openai\":{\"type\":\"api_key\",\"key\":\"sk-stored\"}}\n")
	authPath := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(authPath, authBlob, 0o600); err != nil {
		t.Fatal(err)
	}

	var n int
	srv, hits := federationServer(t, func() (string, int) {
		n++
		return "federated-token", 3600
	})
	setFederationEnv(t, srv.URL, tokenPath, map[string]string{
		"ANTHROPIC_SERVICE_ACCOUNT_ID": "svac_test",
		"ANTHROPIC_WORKSPACE_ID":       "wrkspc_test",
	})

	p, ok := Lookup("anthropic")
	if !ok {
		t.Fatal("anthropic provider missing")
	}
	store := Open(dir)
	chk := CheckAuth(store, "anthropic")
	if chk == nil || chk.Source != federationSource || chk.Type != TypeAPIKey {
		t.Fatalf("check = %+v", chk)
	}
	if got := federationHits(hits, "/v1/oauth/token"); len(got) != 0 {
		t.Fatalf("CheckAuth exchanged %d times", len(got))
	}

	res, err := Resolve(context.Background(), store, p, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Source != federationSource {
		t.Fatalf("result = %+v", res)
	}
	if res.Auth.APIKey != "" {
		t.Fatalf("api key = %q", res.Auth.APIKey)
	}
	if got := res.Auth.Headers["Authorization"]; got != "Bearer federated-token" {
		t.Fatalf("authorization = %q", got)
	}
	if res.Auth.BaseURL != srv.URL {
		t.Fatalf("base = %q", res.Auth.BaseURL)
	}
	tokenHits := federationHits(hits, "/v1/oauth/token")
	if len(tokenHits) != 1 {
		t.Fatalf("token requests = %d", len(tokenHits))
	}
	hit := tokenHits[0]
	if hit.Method != http.MethodPost {
		t.Fatalf("method = %s", hit.Method)
	}
	if hit.Beta != federationBetaHeader {
		t.Fatalf("beta = %q", hit.Beta)
	}
	if !strings.Contains(hit.ContentType, "application/json") {
		t.Fatalf("content-type = %q", hit.ContentType)
	}
	if hit.Body["grant_type"] != federationGrantType ||
		hit.Body["assertion"] != "header.payload.signature" ||
		hit.Body["federation_rule_id"] != "fdrl_test" ||
		hit.Body["organization_id"] != "org-test" ||
		hit.Body["service_account_id"] != "svac_test" ||
		hit.Body["workspace_id"] != "wrkspc_test" {
		t.Fatalf("body = %#v", hit.Body)
	}

	if err := os.WriteFile(tokenPath, []byte("replaced.jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	res2, err := Resolve(context.Background(), store, p, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got := res2.Auth.Headers["Authorization"]; got != "Bearer federated-token" {
		t.Fatalf("cached authorization = %q", got)
	}
	if got := federationHits(hits, "/v1/oauth/token"); len(got) != 1 {
		t.Fatalf("token requests after cache = %d", len(got))
	}
	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, authBlob) {
		t.Fatalf("auth.json changed: %s", got)
	}
}

func TestFederationOptionalIDsOmitted(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, hits := federationServer(t, func() (string, int) { return "tok", 3600 })
	setFederationEnv(t, srv.URL, tokenPath, nil)
	p, _ := Lookup("anthropic")
	if _, err := Resolve(context.Background(), Open(dir), p, ResolveOpts{}); err != nil {
		t.Fatal(err)
	}
	tokenHits := federationHits(hits, "/v1/oauth/token")
	if len(tokenHits) != 1 {
		t.Fatalf("token requests = %d", len(tokenHits))
	}
	if _, ok := tokenHits[0].Body["service_account_id"]; ok {
		t.Fatalf("service account present: %#v", tokenHits[0].Body)
	}
	if _, ok := tokenHits[0].Body["workspace_id"]; ok {
		t.Fatalf("workspace present: %#v", tokenHits[0].Body)
	}
}

func TestFederationPartialEnvDoesNotExchange(t *testing.T) {
	dir := t.TempDir()
	srv, hits := federationServer(t, func() (string, int) { return "tok", 3600 })
	setFederationEnv(t, srv.URL, filepath.Join(dir, "missing.jwt"), nil)
	t.Setenv("ANTHROPIC_IDENTITY_TOKEN_FILE", "")
	p, _ := Lookup("anthropic")
	res, err := Resolve(context.Background(), Open(dir), p, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("result = %+v", res)
	}
	if chk := CheckAuth(Open(dir), "anthropic"); chk != nil {
		t.Fatalf("check = %+v", chk)
	}
	if got := federationHits(hits, "/v1/oauth/token"); len(got) != 0 {
		t.Fatalf("token requests = %d", len(got))
	}
}

func TestFederationAPIKeyWins(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, hits := federationServer(t, func() (string, int) { return "tok", 3600 })
	setFederationEnv(t, srv.URL, tokenPath, map[string]string{"ANTHROPIC_API_KEY": "sk-live"})
	p, _ := Lookup("anthropic")
	res, err := Resolve(context.Background(), Open(dir), p, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Auth.APIKey != "sk-live" || res.Source != "ANTHROPIC_API_KEY" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Auth.Headers) != 0 {
		t.Fatalf("headers = %#v", res.Auth.Headers)
	}
	if got := federationHits(hits, "/v1/oauth/token"); len(got) != 0 {
		t.Fatalf("token requests = %d", len(got))
	}
}

func TestFederationStoredKeyWins(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetAPIKey(dir, "anthropic", "sk-stored"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv, hits := federationServer(t, func() (string, int) { return "tok", 3600 })
	setFederationEnv(t, srv.URL, tokenPath, nil)
	p, _ := Lookup("anthropic")
	res, err := Resolve(context.Background(), Open(dir), p, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Auth.APIKey != "sk-stored" {
		t.Fatalf("result = %+v", res)
	}
	if got := federationHits(hits, "/v1/oauth/token"); len(got) != 0 {
		t.Fatalf("token requests = %d", len(got))
	}
	after, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("auth.json changed: %s", after)
	}
}

func TestFederationRefreshesInsideWindowAndRereadsFile(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("jwt-one"), 0o600); err != nil {
		t.Fatal(err)
	}
	var n int
	srv, hits := federationServer(t, func() (string, int) {
		n++
		return "tok-" + string(rune('0'+n)), 60
	})
	setFederationEnv(t, srv.URL, tokenPath, nil)
	p, _ := Lookup("anthropic")
	store := Open(dir)
	res, err := Resolve(context.Background(), store, p, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Auth.Headers["Authorization"]; got != "Bearer tok-1" {
		t.Fatalf("authorization = %q", got)
	}
	if err := os.WriteFile(tokenPath, []byte("jwt-two"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err = Resolve(context.Background(), store, p, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Auth.Headers["Authorization"]; got != "Bearer tok-2" {
		t.Fatalf("authorization = %q", got)
	}
	tokenHits := federationHits(hits, "/v1/oauth/token")
	if len(tokenHits) != 2 {
		t.Fatalf("token requests = %d", len(tokenHits))
	}
	if tokenHits[0].Body["assertion"] != "jwt-one" || tokenHits[1].Body["assertion"] != "jwt-two" {
		t.Fatalf("assertions = %#v %#v", tokenHits[0].Body, tokenHits[1].Body)
	}
}

func TestFederationAssertionLimit(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, bytes.Repeat([]byte("a"), federationAssertionLimit+1), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, hits := federationServer(t, func() (string, int) { return "tok", 3600 })
	setFederationEnv(t, srv.URL, tokenPath, nil)
	p, _ := Lookup("anthropic")
	_, err := Resolve(context.Background(), Open(dir), p, ResolveOpts{})
	if err == nil || !IsFederationError(err) {
		t.Fatal(err)
	}
	if got := federationHits(hits, "/v1/oauth/token"); len(got) != 0 {
		t.Fatalf("token requests = %d", len(got))
	}
}

func TestFederationRejectsCleartextNonLoopback(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	setFederationEnv(t, "http://example.com", tokenPath, nil)
	p, _ := Lookup("anthropic")
	_, err := Resolve(context.Background(), Open(dir), p, ResolveOpts{})
	if err == nil || !IsFederationError(err) || !strings.Contains(err.Error(), "loopback") {
		t.Fatal(err)
	}
}

func TestFederationDefaultBaseURL(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "")
	base, fromEnv, err := federationBaseURL()
	if err != nil || fromEnv || base != defaultAnthropicFedBase {
		t.Fatalf("base=%q fromEnv=%v err=%v", base, fromEnv, err)
	}
}

func TestFederationIgnoredForOtherProviders(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity.jwt")
	if err := os.WriteFile(tokenPath, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, hits := federationServer(t, func() (string, int) { return "tok", 3600 })
	setFederationEnv(t, srv.URL, tokenPath, nil)
	t.Setenv("OPENAI_API_KEY", "")
	p, _ := Lookup("openai")
	res, err := Resolve(context.Background(), Open(dir), p, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("result = %+v", res)
	}
	if got := federationHits(hits, "/v1/oauth/token"); len(got) != 0 {
		t.Fatalf("token requests = %d", len(got))
	}
}

type federationHit struct {
	Method      string
	Path        string
	Beta        string
	ContentType string
	Body        map[string]any
}

func federationServer(t *testing.T, mint func() (string, int)) (*httptest.Server, *[]federationHit) {
	t.Helper()
	var mu sync.Mutex
	hits := &[]federationHit{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit := federationHit{
			Method:      r.Method,
			Path:        r.URL.Path,
			Beta:        r.Header.Get("anthropic-beta"),
			ContentType: r.Header.Get("Content-Type"),
		}
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &hit.Body)
		}
		mu.Lock()
		*hits = append(*hits, hit)
		mu.Unlock()
		if r.URL.Path != "/v1/oauth/token" {
			http.NotFound(w, r)
			return
		}
		token, expires := mint()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires_in": expires})
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func federationHits(hits *[]federationHit, path string) []federationHit {
	var out []federationHit
	for _, hit := range *hits {
		if hit.Path == path {
			out = append(out, hit)
		}
	}
	return out
}

func setFederationEnv(t *testing.T, base, tokenFile string, extra map[string]string) {
	t.Helper()
	ClearProcessAPIKey("anthropic")
	t.Cleanup(func() { ClearProcessAPIKey("anthropic") })
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
