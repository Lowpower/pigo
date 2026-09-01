package auth

import (
	"net/http"
	"net/http/httptest"
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
	if a.Headers["originator"] != "pi" || a.Headers["OpenAI-Beta"] != "responses=experimental" {
		t.Fatalf("headers = %#v", a.Headers)
	}
}

func TestKimiToAuthBearer(t *testing.T) {
	a, err := kimiOAuth{}.ToAuth(Credential{Access: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Headers["Authorization"] != "Bearer abc" {
		t.Fatalf("%+v", a)
	}
}

func TestCopilotBaseURLFromToken(t *testing.T) {
	u := copilotBaseURL("tid=x;proxy-ep=proxy.individual.githubcopilot.com;exp=1", "")
	if u != "https://api.individual.githubcopilot.com" {
		t.Fatalf("%s", u)
	}
}

func TestParseAuthInput(t *testing.T) {
	code, state := parseAuthInput("http://localhost:53692/callback?code=abc&state=ver")
	if code != "abc" || state != "ver" {
		t.Fatalf("%s %s", code, state)
	}
	code, state = parseAuthInput("abc#ver")
	if code != "abc" || state != "ver" {
		t.Fatalf("hash %s %s", code, state)
	}
}
