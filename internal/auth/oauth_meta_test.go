package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/models"
)

func TestMetaLoginMintsKey(t *testing.T) {
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/device/authorization/"):
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"device_code":"dev","user_code":"ABCD-1234","verification_uri_complete":"https://auth.meta.com/oauth/device/?code=ABCD-1234","interval":1,"expires_in":30}`))
		case strings.HasSuffix(r.URL.Path, "/device/token/"):
			polls++
			w.Header().Set("content-type", "application/json")
			if polls == 1 {
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"identity-token"}`))
		default:
			if r.Header.Get("authorization") != "Bearer identity-token" || r.Header.Get("x-api-version") != "1.0.0" {
				t.Errorf("mint headers = %v", r.Header)
			}
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"api_key":"mk-1"}`))
		}
	}))
	defer srv.Close()
	restore := swapMetaURLs(srv.URL)
	defer restore()

	var sawCode, sawProgress bool
	cred, err := metaOAuth{}.Login(Interaction{Notify: func(ev Event) {
		if ev.Type == EventDeviceCode && ev.UserCode == "ABCD-1234" && strings.Contains(ev.VerificationURI, "ABCD-1234") {
			sawCode = true
		}
		if ev.Type == EventProgress {
			sawProgress = true
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !sawCode || !sawProgress {
		t.Fatalf("notify code=%v progress=%v", sawCode, sawProgress)
	}
	if cred.Type != TypeOAuth || cred.Access != "mk-1" || cred.Refresh != "identity-token" {
		t.Fatalf("cred = %+v", cred)
	}
	if cred.Expires < time.Now().Add(23*time.Hour).UnixMilli() {
		t.Fatalf("expires = %d", cred.Expires)
	}
	auth, err := metaOAuth{}.ToAuth(cred)
	if err != nil || auth.APIKey != "mk-1" {
		t.Fatalf("auth = %+v err=%v", auth, err)
	}
}

func TestMetaRefreshRemints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer identity" {
			t.Errorf("auth = %s", r.Header.Get("authorization"))
		}
		_, _ = w.Write([]byte(`{"api_key":"mk-2"}`))
	}))
	defer srv.Close()
	restore := swapMetaURLs(srv.URL)
	defer restore()

	cred, err := metaOAuth{}.Refresh(t.Context(), Credential{Type: TypeOAuth, Refresh: "identity", Access: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if cred.Access != "mk-2" || cred.Refresh != "identity" {
		t.Fatalf("cred = %+v", cred)
	}
}

func TestMetaMintSetupURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"action_url":"https://api.meta.ai/setup"}`))
	}))
	defer srv.Close()
	restore := swapMetaURLs(srv.URL)
	defer restore()
	_, err := mintMetaKey(t.Context(), "identity")
	if err == nil || !strings.Contains(err.Error(), "https://api.meta.ai/setup") {
		t.Fatalf("err = %v", err)
	}
}

func TestMetaMintExpiredSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer srv.Close()
	restore := swapMetaURLs(srv.URL)
	defer restore()
	_, err := metaOAuth{}.Refresh(t.Context(), Credential{Refresh: "identity"})
	if err == nil || !strings.Contains(err.Error(), "status 401") || !strings.Contains(err.Error(), "/login meta") {
		t.Fatalf("err = %v", err)
	}
}

func TestMetaProviderRegistered(t *testing.T) {
	p, ok := Lookup("meta")
	if !ok || p.OAuth == nil || p.APIKey == nil {
		t.Fatalf("auth provider = %+v ok=%v", p, ok)
	}
	found := false
	for _, env := range p.APIKey.Env {
		if env == "META_API_KEY" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env = %v", p.APIKey.Env)
	}
	spec, ok := models.LookupProvider("meta")
	if !ok || spec.DefaultAPI != "openai-responses" || spec.DefaultID != "muse-spark-1.3" || spec.BaseURL != "https://api.meta.ai/v1" {
		t.Fatalf("spec = %+v ok=%v", spec, ok)
	}
}

func swapMetaURLs(base string) func() {
	oldDevice, oldToken, oldMint := metaDeviceURL, metaTokenURL, metaMintURL
	metaDeviceURL = base + "/oidc/device/authorization/"
	metaTokenURL = base + "/oidc/device/token/"
	metaMintURL = base + "/muse-code/key"
	return func() {
		metaDeviceURL, metaTokenURL, metaMintURL = oldDevice, oldToken, oldMint
	}
}
