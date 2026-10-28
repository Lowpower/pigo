package auth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/models"
)

const (
	chatGPTDevice = "11111111-1111-4111-8111-111111111111"
	chatGPTScope  = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
)

func TestOpenAILoginLabels(t *testing.T) {
	openai, ok := Lookup("openai")
	if !ok || openai.OAuth == nil {
		t.Fatal("openai has no OAuth login")
	}
	if openai.OAuth.LoginLabel() != "Sign in with ChatGPT" {
		t.Fatalf("label = %q", openai.OAuth.LoginLabel())
	}
	if openai.OAuth.Name() != "OpenAI (ChatGPT subscription)" || !openai.OAuth.IsSubscription() {
		t.Fatalf("name=%q subscription=%v", openai.OAuth.Name(), openai.OAuth.IsSubscription())
	}
	codex, ok := Lookup("openai-codex")
	if !ok || codex.OAuth == nil || codex.OAuth.Name() != "OpenAI Codex (legacy)" {
		t.Fatalf("codex name = %q", codex.OAuth.Name())
	}
	spec, ok := models.LookupProvider("openai-codex")
	if !ok || spec.Name != "OpenAI Codex (legacy)" {
		t.Fatalf("catalog name = %q", spec.Name)
	}
}

func TestChatGPTLoginStoresSubscriptionToken(t *testing.T) {
	t.Setenv("PIGO_OAUTH_CALLBACK_HOST", "127.0.0.1")
	var got url.Values
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("form: %v", err)
		}
		got = r.PostForm
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"sub-token","refresh_token":"sub-refresh","expires_in":3600,"id_token":"id-token","scope":"` + chatGPTScope + `"}`))
	}))
	t.Cleanup(tokenSrv.Close)
	old := openaiChatGPTTokenURL
	openaiChatGPTTokenURL = tokenSrv.URL
	t.Cleanup(func() { openaiChatGPTTokenURL = old })

	dir := t.TempDir()
	store := Open(dir)
	codex := Credential{Type: TypeOAuth, Access: "codex-access", Refresh: "codex-refresh", Expires: 99}
	if _, err := store.Modify("openai-codex", func(*Credential) (*Credential, error) {
		return &codex, nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var authRaw string
	err = Login(ctx, store, "openai", TypeOAuth, Interaction{
		Ctx: ctx,
		DeviceID: func() (string, error) {
			return strings.ToUpper(chatGPTDevice), nil
		},
		Notify: func(ev Event) {
			if ev.Type != EventAuthURL {
				return
			}
			authRaw = ev.URL
			authURL, err := url.Parse(ev.URL)
			if err != nil {
				t.Errorf("auth url: %v", err)
				return
			}
			q := authURL.Query()
			cb, err := url.Parse("http://127.0.0.1:1455/auth/callback")
			if err != nil {
				t.Errorf("callback: %v", err)
				return
			}
			cq := cb.Query()
			cq.Set("code", "browser-code")
			cq.Set("state", q.Get("state"))
			cq.Set("client_id", "issued-1")
			cb.RawQuery = cq.Encode()
			resp, err := http.Get(cb.String())
			if err != nil {
				t.Errorf("callback get: %v", err)
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
	authURL, err := url.Parse(authRaw)
	if err != nil {
		t.Fatal(err)
	}
	q := authURL.Query()
	if authURL.Scheme != "https" || authURL.Host != "auth.openai.com" || authURL.Path != "/api/accounts/authorize" {
		t.Fatalf("authorize = %s", authRaw)
	}
	if q.Get("client_id") != "dynamic_agent_client" || q.Get("agent_name_hint") != "pigo" {
		t.Fatalf("authorize query = %s", q.Encode())
	}
	if q.Get("ext_agent_host_id") != "urn:uuid:"+chatGPTDevice {
		t.Fatalf("host id = %q", q.Get("ext_agent_host_id"))
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:1455/auth/callback" || q.Get("resource") != "https://api.openai.com/v1" {
		t.Fatalf("redirect/resource = %s", q.Encode())
	}
	if q.Get("scope") != chatGPTScope || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
		t.Fatalf("scope/challenge = %s", q.Encode())
	}
	if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge") == "" {
		t.Fatalf("missing pkce fields: %s", q.Encode())
	}
	if got.Get("grant_type") != "authorization_code" || got.Get("client_id") != "issued-1" || got.Get("code") != "browser-code" {
		t.Fatalf("token form = %s", got.Encode())
	}
	if got.Get("redirect_uri") != "http://127.0.0.1:1455/auth/callback" || got.Get("resource") != "https://api.openai.com/v1" || got.Get("code_verifier") == "" {
		t.Fatalf("token form = %s", got.Encode())
	}
	stored, ok, err := store.Read("openai")
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if stored.Type != TypeOAuth || stored.Access != "sub-token" || stored.Refresh != "sub-refresh" {
		t.Fatalf("%+v", stored)
	}
	if stored.extraString("clientId") != "issued-1" {
		t.Fatalf("clientId = %q", stored.extraString("clientId"))
	}
	if scopes := extraStringSlice(stored, "scopes"); len(scopes) == 0 || scopes[len(scopes)-1] != "chatgpt.tokens.use.direct" {
		t.Fatalf("scopes = %#v", scopes)
	}
	margin := int64((3 * time.Minute) / time.Millisecond)
	if stored.Expires > time.Now().Add(time.Hour).UnixMilli()-margin+2000 {
		t.Fatalf("expires missing margin: %d", stored.Expires)
	}
	authn, err := openaiChatGPTOAuth{}.ToAuth(stored)
	if err != nil {
		t.Fatal(err)
	}
	if authn.APIKey != "sub-token" || len(authn.Headers) != 0 || authn.BaseURL != "" {
		t.Fatalf("toAuth = %+v", authn)
	}
	kept, ok, err := store.Read("openai-codex")
	if err != nil || !ok || kept.Access != "codex-access" || kept.Refresh != "codex-refresh" || kept.Expires != 99 {
		t.Fatalf("codex rewritten: ok=%v err=%v cred=%+v", ok, err, kept)
	}
	raw, err := os.ReadFile(store.path())
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	var codexRaw map[string]any
	if err := json.Unmarshal(file["openai-codex"], &codexRaw); err != nil {
		t.Fatal(err)
	}
	if codexRaw["access"] != "codex-access" {
		t.Fatalf("file codex = %#v\nbefore %s", codexRaw, before)
	}
}

func TestChatGPTLoginRejectsBadToken(t *testing.T) {
	t.Setenv("PIGO_OAUTH_CALLBACK_HOST", "127.0.0.1")
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "scope",
			body: `{"access_token":"a","refresh_token":"r","expires_in":3600,"id_token":"id","scope":"openid profile email"}`,
			want: "chatgpt.tokens.use.direct",
		},
		{
			name: "id-token",
			body: `{"access_token":"a","refresh_token":"r","expires_in":3600,"scope":"` + chatGPTScope + `"}`,
			want: "ID token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("content-type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(tokenSrv.Close)
			old := openaiChatGPTTokenURL
			openaiChatGPTTokenURL = tokenSrv.URL
			t.Cleanup(func() { openaiChatGPTTokenURL = old })

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := openaiChatGPTOAuth{}.Login(Interaction{
				Ctx:      ctx,
				DeviceID: func() (string, error) { return chatGPTDevice, nil },
				Notify: func(ev Event) {
					if ev.Type != EventAuthURL {
						return
					}
					authURL, err := url.Parse(ev.URL)
					if err != nil {
						t.Errorf("auth url: %v", err)
						return
					}
					cb, _ := url.Parse("http://127.0.0.1:1455/auth/callback")
					q := cb.Query()
					q.Set("code", "browser-code")
					q.Set("state", authURL.Query().Get("state"))
					q.Set("client_id", "issued-1")
					cb.RawQuery = q.Encode()
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
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestChatGPTPasteRejectsForeignURL(t *testing.T) {
	t.Setenv("PIGO_OAUTH_CALLBACK_HOST", "127.0.0.1")
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("token exchange should not run")
	}))
	t.Cleanup(tokenSrv.Close)
	old := openaiChatGPTTokenURL
	openaiChatGPTTokenURL = tokenSrv.URL
	t.Cleanup(func() { openaiChatGPTTokenURL = old })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := openaiChatGPTOAuth{}.Login(Interaction{
		Ctx:      ctx,
		DeviceID: func() (string, error) { return chatGPTDevice, nil },
		Prompt: func(Prompt) (string, error) {
			return "https://evil.example/auth/callback?code=1&state=x&client_id=c", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "http://127.0.0.1:1455/auth/callback") {
		t.Fatalf("error = %v", err)
	}
}

func TestChatGPTLoginFailsWhenCallbackPortTaken(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opened := false
	_, err = openaiChatGPTOAuth{}.Login(Interaction{
		Ctx:      ctx,
		DeviceID: func() (string, error) { return chatGPTDevice, nil },
		Notify: func(ev Event) {
			if ev.Type == EventAuthURL {
				opened = true
			}
		},
	})
	if err == nil {
		t.Fatal("expected port in use")
	}
	if !strings.Contains(err.Error(), "port 1455 is in use") || strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("error = %v", err)
	}
	if opened {
		t.Fatal("opened browser")
	}
}

func TestChatGPTLoginChecksDeviceIDBeforeBind(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	_, err = openaiChatGPTOAuth{}.Login(Interaction{
		DeviceID: func() (string, error) { return "not-a-uuid", nil },
	})
	if err == nil || !strings.Contains(err.Error(), "device ID") || strings.Contains(err.Error(), "in use") {
		t.Fatalf("error = %v", err)
	}
}

func TestChatGPTRefreshUsesIssuedClient(t *testing.T) {
	var got url.Values
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("form: %v", err)
		}
		got = r.PostForm
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"next","refresh_token":"next-refresh","expires_in":3600,"scope":"` + chatGPTScope + `"}`))
	}))
	t.Cleanup(tokenSrv.Close)
	old := openaiChatGPTTokenURL
	openaiChatGPTTokenURL = tokenSrv.URL
	t.Cleanup(func() { openaiChatGPTTokenURL = old })

	cred := Credential{Type: TypeOAuth, Access: "old", Refresh: "old-refresh"}.withExtra("clientId", "issued-9")
	before := time.Now()
	gotCred, err := openaiChatGPTOAuth{}.Refresh(t.Context(), cred)
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("grant_type") != "refresh_token" || got.Get("client_id") != "issued-9" || got.Get("refresh_token") != "old-refresh" {
		t.Fatalf("form = %s", got.Encode())
	}
	if got.Get("resource") != "https://api.openai.com/v1" {
		t.Fatalf("resource = %q", got.Get("resource"))
	}
	if gotCred.Access != "next" || gotCred.Refresh != "next-refresh" || gotCred.extraString("clientId") != "issued-9" {
		t.Fatalf("%+v", gotCred)
	}
	lo := before.Add(time.Hour-3*time.Minute).UnixMilli() - 1000
	hi := after.Add(time.Hour-3*time.Minute).UnixMilli() + 1000
	if gotCred.Expires < lo || gotCred.Expires > hi {
		t.Fatalf("expires %d not in [%d,%d]", gotCred.Expires, lo, hi)
	}

	_, err = openaiChatGPTOAuth{}.Refresh(t.Context(), Credential{Type: TypeOAuth, Refresh: "r"})
	if err == nil || !strings.Contains(err.Error(), "client ID") {
		t.Fatalf("error = %v", err)
	}
}
