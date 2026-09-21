package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetaToAuthUsesMintedKey(t *testing.T) {
	a, err := metaOAuth{}.ToAuth(Credential{Access: "LLM|key"})
	if err != nil {
		t.Fatal(err)
	}
	if a.APIKey != "LLM|key" || len(a.Headers) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestMetaRefreshRemintsAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/muse-code/key" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("authorization") != "Bearer identity-token" {
			t.Errorf("authorization = %q", r.Header.Get("authorization"))
		}
		if r.Header.Get("x-api-version") != metaAPIVersion {
			t.Errorf("x-api-version = %q", r.Header.Get("x-api-version"))
		}
		body, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(body)) != "{}" {
			t.Errorf("body = %q", body)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"LLM|fresh-key"}`))
	}))
	defer srv.Close()
	old := metaMintURL
	metaMintURL = srv.URL + "/muse-code/key"
	defer func() { metaMintURL = old }()

	before := time.Now().UnixMilli()
	got, err := metaOAuth{}.Refresh(t.Context(), Credential{Type: TypeOAuth, Refresh: "identity-token", Access: "LLM|old"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeOAuth || got.Access != "LLM|fresh-key" || got.Refresh != "identity-token" {
		t.Fatalf("%+v", got)
	}
	wantMin := before + metaAPIKeyLifetime.Milliseconds() - time.Second.Milliseconds()
	if got.Expires < wantMin {
		t.Fatalf("expires %d, want >= %d", got.Expires, wantMin)
	}
}

func TestMetaMintReportsSetupURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"require_payment":true,"action_url":"https://dev.meta.ai/billing"}`))
	}))
	defer srv.Close()
	old := metaMintURL
	metaMintURL = srv.URL
	defer func() { metaMintURL = old }()

	_, err := metaOAuth{}.Refresh(t.Context(), Credential{Refresh: "identity-token"})
	if err == nil || !strings.Contains(err.Error(), "Complete setup at https://dev.meta.ai/billing") {
		t.Fatalf("err = %v", err)
	}
}

func TestMetaMintExpiredSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer srv.Close()
	old := metaMintURL
	metaMintURL = srv.URL
	defer func() { metaMintURL = old }()

	_, err := metaOAuth{}.Refresh(t.Context(), Credential{Refresh: "stale"})
	if err == nil || !strings.Contains(err.Error(), "session expired") || !strings.Contains(err.Error(), "/login meta") {
		t.Fatalf("err = %v", err)
	}
}

func TestMetaLoginDeviceFlowMintsKey(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/authorization/") || strings.HasSuffix(r.URL.Path, "/authorization"):
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("client_id") != metaClientID {
				t.Errorf("client_id = %q", r.Form.Get("client_id"))
			}
			_, _ = w.Write([]byte(`{
				"device_code":"device-code-123",
				"user_code":"ABCD-1234",
				"verification_uri":"https://auth.meta.com/oauth/device/",
				"verification_uri_complete":"https://auth.meta.com/oauth/device/?code=ABCD-1234",
				"interval":1,
				"expires_in":60
			}`))
		case strings.HasSuffix(r.URL.Path, "/token/") || strings.HasSuffix(r.URL.Path, "/token"):
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
			}
			if r.Form.Get("device_code") != "device-code-123" {
				t.Errorf("device_code = %q", r.Form.Get("device_code"))
			}
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"identity-token","token_type":"Bearer"}`))
		case strings.HasSuffix(r.URL.Path, "/muse-code/key"):
			if r.Header.Get("authorization") != "Bearer identity-token" {
				t.Errorf("authorization = %q", r.Header.Get("authorization"))
			}
			_, _ = w.Write([]byte(`{"api_key":"LLM|minted-key"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	oldDevice, oldToken, oldMint := metaDeviceURL, metaTokenURL, metaMintURL
	metaDeviceURL = srv.URL + "/oidc/device/authorization/"
	metaTokenURL = srv.URL + "/oidc/device/token/"
	metaMintURL = srv.URL + "/muse-code/key"
	defer func() {
		metaDeviceURL, metaTokenURL, metaMintURL = oldDevice, oldToken, oldMint
	}()

	var events []Event
	got, err := metaOAuth{}.Login(Interaction{
		Ctx: t.Context(),
		Notify: func(ev Event) {
			events = append(events, ev)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Access != "LLM|minted-key" || got.Refresh != "identity-token" || got.Type != TypeOAuth {
		t.Fatalf("%+v", got)
	}
	if len(events) < 2 || events[0].Type != EventDeviceCode || events[0].UserCode != "ABCD-1234" {
		t.Fatalf("events = %+v", events)
	}
	if events[0].VerificationURI != "https://auth.meta.com/oauth/device/?code=ABCD-1234" {
		t.Fatalf("uri = %q", events[0].VerificationURI)
	}
	foundProgress := false
	for _, ev := range events {
		if ev.Type == EventProgress && strings.Contains(ev.Message, "Meta Model API") {
			foundProgress = true
		}
	}
	if !foundProgress {
		t.Fatalf("missing mint progress: %+v", events)
	}
}

func TestMetaProviderRegistered(t *testing.T) {
	p, ok := Lookup("meta")
	if !ok || p.OAuth == nil {
		t.Fatalf("meta oauth missing: %+v", p)
	}
	if p.APIKey == nil || len(p.APIKey.Env) == 0 || p.APIKey.Env[0] != "META_API_KEY" {
		t.Fatalf("meta api key = %+v", p.APIKey)
	}
	if p.OAuth.Name() != "Meta (Muse subscription)" || !p.OAuth.IsSubscription() {
		t.Fatalf("oauth labels = %q sub=%v", p.OAuth.Name(), p.OAuth.IsSubscription())
	}
}
