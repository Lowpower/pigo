package auth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAnthropicRefreshHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer srv.Close()
	old := anthropicTokenURL
	anthropicTokenURL = srv.URL
	defer func() { anthropicTokenURL = old }()

	got, err := anthropicOAuth{}.Refresh(t.Context(), Credential{Type: TypeOAuth, Refresh: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Access != "new-access" || got.Refresh != "new-refresh" {
		t.Fatalf("%+v", got)
	}
	if got.Expires < time.Now().UnixMilli() {
		t.Fatalf("expires %d", got.Expires)
	}
}

func TestOpenRouterRefreshNoop(t *testing.T) {
	in := Credential{Type: TypeOAuth, Access: "perm", Refresh: "", Expires: 1<<62 - 1}
	got, err := openrouterOAuth{}.Refresh(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Access != "perm" {
		t.Fatalf("%+v", got)
	}
}

func TestCodexToAuthHeaders(t *testing.T) {
	a, err := openaiCodexOAuth{}.ToAuth(Credential{Access: "tok", Extra: map[string]any{"accountId": "acct-9"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.APIKey != "tok" {
		t.Fatalf("apiKey = %q", a.APIKey)
	}
	if a.Headers["chatgpt-account-id"] != "acct-9" {
		t.Fatalf("headers = %#v", a.Headers)
	}
	if a.Headers["originator"] != "pigo" || a.Headers["OpenAI-Beta"] != "responses=experimental" {
		t.Fatalf("headers = %#v", a.Headers)
	}
}

func TestOAuthToAuthBearer(t *testing.T) {
	tests := []struct {
		name string
		p    interface {
			ToAuth(Credential) (ModelAuth, error)
		}
		access string
	}{
		{"anthropic", anthropicOAuth{}, "sk-ant-oat-1"},
		{"kimi", kimiOAuth{}, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := tt.p.ToAuth(Credential{Access: tt.access})
			if err != nil {
				t.Fatal(err)
			}
			if a.Headers["Authorization"] != "Bearer "+tt.access {
				t.Fatalf("%+v", a)
			}
		})
	}
}

func TestAnthropicAuthTokenResolveIsBearer(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "auth-token")
	p, ok := Lookup("anthropic")
	if !ok {
		t.Fatal("missing anthropic")
	}
	res, err := Resolve(context.Background(), Open(t.TempDir()), p, ResolveOpts{})
	if err != nil || res == nil {
		t.Fatal(err, res)
	}
	if res.Source != "ANTHROPIC_AUTH_TOKEN" {
		t.Fatalf("source = %q", res.Source)
	}
	if res.Auth.APIKey != "" {
		t.Fatalf("AUTH_TOKEN leaked as APIKey: %+v", res.Auth)
	}
	if res.Auth.Headers["Authorization"] != "Bearer auth-token" {
		t.Fatalf("headers = %#v", res.Auth.Headers)
	}
}

func TestAnthropicOAuthTokenResolvePrefersOverAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-api")
	t.Setenv("ANTHROPIC_OAUTH_TOKEN", "sk-ant-oat-env")
	p, ok := Lookup("anthropic")
	if !ok {
		t.Fatal("missing anthropic")
	}
	res, err := Resolve(context.Background(), Open(t.TempDir()), p, ResolveOpts{})
	if err != nil || res == nil {
		t.Fatal(err, res)
	}
	if res.Source != "ANTHROPIC_OAUTH_TOKEN" || res.Auth.APIKey != "sk-ant-oat-env" {
		t.Fatalf("%+v", res)
	}
}

func TestCopilotBaseURLFromToken(t *testing.T) {
	u := copilotBaseURL("tid=x;proxy-ep=proxy.individual.githubcopilot.com;exp=1", "")
	if u != "https://api.individual.githubcopilot.com" {
		t.Fatalf("%s", u)
	}
}

func TestParseAuthInput(t *testing.T) {
	code, state, err := parseAuthInput("http://localhost:53692/callback?code=abc&state=ver")
	if err != nil || code != "abc" || state != "ver" {
		t.Fatalf("%s %s %v", code, state, err)
	}
	code, state, err = parseAuthInput("abc#ver")
	if err != nil || code != "abc" || state != "ver" {
		t.Fatalf("hash %s %s %v", code, state, err)
	}
	_, _, err = parseAuthInput("http://localhost/callback?error=access_denied&error_description=nope")
	if err == nil || !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnthropicLoginFallsBackWhenPreferredPortTaken(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:53692")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	var exchangedRedirect string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RedirectURI string `json:"redirect_uri"`
			Code        string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.Code != "browser-code" {
			t.Errorf("code = %q", body.Code)
		}
		exchangedRedirect = body.RedirectURI
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
	}))
	t.Cleanup(tokenSrv.Close)
	old := anthropicTokenURL
	anthropicTokenURL = tokenSrv.URL
	t.Cleanup(func() { anthropicTokenURL = old })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var redirect string
	cred, err := anthropicOAuth{}.Login(Interaction{
		Ctx: ctx,
		Notify: func(ev Event) {
			if ev.Type != EventAuthURL {
				return
			}
			authURL, err := url.Parse(ev.URL)
			if err != nil {
				t.Errorf("auth url: %v", err)
				return
			}
			redirect = authURL.Query().Get("redirect_uri")
			cb, err := url.Parse(redirect)
			if err != nil {
				t.Errorf("redirect: %v", err)
				return
			}
			q := cb.Query()
			q.Set("code", "browser-code")
			q.Set("state", authURL.Query().Get("state"))
			cb.RawQuery = q.Encode()
			cb.Host = net.JoinHostPort("127.0.0.1", cb.Port())
			resp, err := http.Get(cb.String())
			if err != nil {
				t.Errorf("callback: %v", err)
				return
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("callback status %d", resp.StatusCode)
			}
		},
		Prompt: func(Prompt) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ru, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	if ru.Hostname() != "localhost" || ru.Path != "/callback" || ru.Port() == "" || ru.Port() == "53692" {
		t.Fatalf("redirect = %s", redirect)
	}
	if exchangedRedirect != redirect {
		t.Fatalf("exchanged %s want %s", exchangedRedirect, redirect)
	}
	if cred.Access != "access" || cred.Refresh != "refresh" {
		t.Fatalf("%+v", cred)
	}
}

func TestCodexLoginFailsWhenCallbackPortTaken(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opened := false
	_, err = openaiCodexOAuth{}.Login(Interaction{
		Ctx: ctx,
		Notify: func(ev Event) {
			if ev.Type == EventAuthURL {
				opened = true
			}
		},
	})
	if err == nil {
		t.Fatal("expected port in use")
	}
	if !strings.Contains(err.Error(), "callback port 1455 is already in use") {
		t.Fatalf("error = %v", err)
	}
	if opened {
		t.Fatal("opened browser")
	}
}

func TestOAuthErrorCallbackEndsLogin(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("token exchange should not run")
	}))
	t.Cleanup(tokenSrv.Close)
	old := anthropicTokenURL
	anthropicTokenURL = tokenSrv.URL
	t.Cleanup(func() { anthropicTokenURL = old })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := anthropicOAuth{}.Login(Interaction{
		Ctx: ctx,
		Notify: func(ev Event) {
			if ev.Type != EventAuthURL {
				return
			}
			authURL, err := url.Parse(ev.URL)
			if err != nil {
				t.Errorf("auth url: %v", err)
				return
			}
			cb, err := url.Parse(authURL.Query().Get("redirect_uri"))
			if err != nil {
				t.Errorf("redirect: %v", err)
				return
			}
			q := url.Values{}
			q.Set("error", "access_denied")
			q.Set("error_description", "nope")
			cb.RawQuery = q.Encode()
			cb.Host = net.JoinHostPort("127.0.0.1", cb.Port())
			resp, err := http.Get(cb.String())
			if err != nil {
				t.Errorf("callback: %v", err)
				return
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.Copy(io.Discard, resp.Body)
		},
		Prompt: func(Prompt) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error = %v", err)
	}
}

func TestPastedOAuthErrorEndsLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := anthropicOAuth{}.Login(Interaction{
		Ctx:    ctx,
		Notify: func(Event) {},
		Prompt: func(Prompt) (string, error) {
			return "http://localhost:53692/callback?error=access_denied&error_description=nope", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("login waited %s", time.Since(start))
	}
}
