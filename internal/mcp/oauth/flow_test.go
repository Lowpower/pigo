package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/auth"
)

func TestSignInRegistersAndToolCallUsesAccessToken(t *testing.T) {
	f := &fakeAS{}
	f.start(t)
	client, _, server := f.client(t, "docs")
	ctx := testCtx(t)
	if err := client.SignIn(ctx, server, Request{Settings: Settings{}, Prompt: drive(t)}); err != nil {
		t.Fatal(err)
	}
	if f.registerPosts.Load() != 1 {
		t.Fatalf("register posts = %d", f.registerPosts.Load())
	}
	var body struct {
		ClientName      string   `json:"client_name"`
		RedirectURIs    []string `json:"redirect_uris"`
		ApplicationType string   `json:"application_type"`
	}
	if err := json.Unmarshal([]byte(f.regBody()), &body); err != nil {
		t.Fatal(err)
	}
	if body.ClientName != "pigo" {
		t.Fatalf("client_name = %q", body.ClientName)
	}
	if body.ApplicationType != "native" || len(body.RedirectURIs) != 1 {
		t.Fatalf("registration = %+v", body)
	}
	req, err := http.NewRequest(http.MethodPost, f.srv.URL+"/tools/call", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Call(ctx, server, req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if got := f.authHeader(); got != "Bearer access-1" {
		t.Fatalf("authorization = %q", got)
	}
	if f.toolHits.Load() != 1 {
		t.Fatalf("tool hits = %d", f.toolHits.Load())
	}
}

func TestClientNameOverridesRegistration(t *testing.T) {
	f := &fakeAS{}
	f.start(t)
	client, _, server := f.client(t, "docs")
	err := client.SignIn(testCtx(t), server, Request{
		Settings: Settings{ClientName: "Pigo Dev"},
		Prompt:   drive(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		ClientName string `json:"client_name"`
	}
	if err := json.Unmarshal([]byte(f.regBody()), &body); err != nil {
		t.Fatal(err)
	}
	if body.ClientName != "Pigo Dev" {
		t.Fatalf("client_name = %q", body.ClientName)
	}
}

func TestIssuerMismatchDoesNotExchange(t *testing.T) {
	f := &fakeAS{iss: "https://evil.example/as"}
	f.start(t)
	client, _, server := f.client(t, "docs")
	err := client.SignIn(testCtx(t), server, Request{Prompt: drive(t)})
	var mismatch *IssuerError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %v", err)
	}
	if f.tokenPosts.Load() != 0 {
		t.Fatalf("token posts = %d", f.tokenPosts.Load())
	}
}

func TestMissingIssDoesNotExchange(t *testing.T) {
	f := &fakeAS{skipIss: true}
	f.start(t)
	client, _, server := f.client(t, "docs")
	err := client.SignIn(testCtx(t), server, Request{Prompt: drive(t)})
	var mismatch *IssuerError
	if !errors.As(err, &mismatch) || mismatch.Got != "" {
		t.Fatalf("err = %v", err)
	}
	if f.tokenPosts.Load() != 0 {
		t.Fatalf("token posts = %d", f.tokenPosts.Load())
	}
}

func TestMissingIssuerMetadataDoesNotExchange(t *testing.T) {
	f := &fakeAS{omitIssuer: true}
	f.start(t)
	client, _, server := f.client(t, "docs")
	err := client.SignIn(testCtx(t), server, Request{Prompt: drive(t)})
	if err == nil {
		t.Fatal("missing issuer succeeded")
	}
	if f.tokenPosts.Load() != 0 {
		t.Fatalf("token posts = %d", f.tokenPosts.Load())
	}
}

func TestStepUpScopeIsUnion(t *testing.T) {
	f := &fakeAS{}
	f.start(t)
	client, store, server := f.client(t, "docs")
	if err := store.Save(server.Name, server.URL, Record{
		Tokens: &Tokens{AccessToken: "old", RefreshToken: "refresh-old", Scope: "read"},
	}); err != nil {
		t.Fatal(err)
	}
	err := client.SignIn(testCtx(t), server, Request{
		Prompt:    drive(t),
		Challenge: Challenge{Error: "insufficient_scope", Scope: "write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.scope() != "read write" {
		t.Fatalf("authorize scope = %q", f.scope())
	}
	grants := f.grantList()
	if len(grants) != 1 || grants[0] != "authorization_code" {
		t.Fatalf("grants = %#v", grants)
	}
	rec, ok, err := store.Load(server.Name, server.URL)
	if err != nil || !ok || rec.Tokens == nil || rec.Tokens.Scope != "read write" {
		t.Fatalf("stored = %+v ok=%v err=%v", rec.Tokens, ok, err)
	}
}

func TestNullOptionalTokenFields(t *testing.T) {
	f := &fakeAS{tokenBody: `{"access_token":"access-null","token_type":null,"refresh_token":null,"scope":"","expires_in":null,"id_token":null}`}
	f.start(t)
	client, store, server := f.client(t, "docs")
	if err := client.SignIn(testCtx(t), server, Request{Prompt: drive(t)}); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := store.Load(server.Name, server.URL)
	if err != nil || !ok || rec.Tokens == nil {
		t.Fatalf("record ok=%v err=%v", ok, err)
	}
	if rec.Tokens.AccessToken != "access-null" || rec.Tokens.RefreshToken != "" || rec.TokensExpireAt != nil {
		t.Fatalf("tokens = %+v expire=%v", rec.Tokens, rec.TokensExpireAt)
	}
}

func TestRefreshKeepsScopeWhenOmitted(t *testing.T) {
	f := &fakeAS{tokenBody: `{"access_token":"new","token_type":"Bearer"}`}
	f.start(t)
	client, store, server := f.client(t, "docs")
	exp := time.Now().Add(-time.Minute).UnixMilli()
	if err := store.Save(server.Name, server.URL, Record{
		ClientID:       "cid",
		RedirectURI:    "http://127.0.0.1/callback",
		Tokens:         &Tokens{AccessToken: "old", RefreshToken: "r1", Scope: "read", TokenType: "Bearer"},
		TokensExpireAt: &exp,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := client.Token(testCtx(t), server)
	if err != nil {
		t.Fatal(err)
	}
	if got != "new" {
		t.Fatalf("token = %q", got)
	}
	rec, ok, err := store.Load(server.Name, server.URL)
	if err != nil || !ok || rec.Tokens == nil {
		t.Fatal(err)
	}
	if rec.Tokens.Scope != "read" || rec.Tokens.RefreshToken != "r1" {
		t.Fatalf("tokens = %+v", rec.Tokens)
	}
}

func TestCancelDuringRefreshDoesNotSend(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := &fakeAS{refreshStarted: started, blockRefresh: release}
	f.start(t)
	client, store, server := f.client(t, "docs")
	exp := time.Now().Add(-time.Minute).UnixMilli()
	if err := store.Save(server.Name, server.URL, Record{
		ClientID:       "cid",
		RedirectURI:    "http://127.0.0.1/callback",
		Tokens:         &Tokens{AccessToken: "old", RefreshToken: "r1", TokenType: "Bearer", Scope: "read"},
		TokensExpireAt: &exp,
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequest(http.MethodPost, f.srv.URL+"/tools/call", nil)
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() {
		_, callErr := client.Call(ctx, server, req)
		errCh <- callErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not start")
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Call err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call waited for refresh")
	}
	if f.toolHits.Load() != 0 {
		t.Fatal("tool call was sent")
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec, ok, err := store.Load(server.Name, server.URL)
		if err == nil && ok && rec.Tokens != nil && rec.Tokens.AccessToken == "rotated" {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("refresh did not save the rotated token")
}

func TestInsufficientScopeDoesNotRefresh(t *testing.T) {
	f := &fakeAS{}
	f.start(t)
	client, store, server := f.client(t, "docs")
	if err := store.Save(server.Name, server.URL, Record{
		Tokens: &Tokens{AccessToken: "old", RefreshToken: "r1", Scope: "read"},
	}); err != nil {
		t.Fatal(err)
	}
	err := client.OnUnauthorized(testCtx(t), server, `Bearer error="insufficient_scope", scope="write"`)
	if !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("err = %v", err)
	}
	if f.tokenPosts.Load() != 0 {
		t.Fatalf("token posts = %d", f.tokenPosts.Load())
	}
}

func TestCIMDSkipsDynamicRegistration(t *testing.T) {
	f := &fakeAS{cimd: true}
	f.start(t)
	client, _, server := f.client(t, "docs")
	err := client.SignIn(testCtx(t), server, Request{
		Settings: Settings{ClientRegistration: "cimd"},
		Prompt:   drive(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.registerPosts.Load() != 0 {
		t.Fatalf("register posts = %d", f.registerPosts.Load())
	}
	if f.clientID() != ClientMetadataURL {
		t.Fatalf("client_id = %s", f.clientID())
	}
	if f.tokenPosts.Load() != 1 {
		t.Fatalf("token posts = %d", f.tokenPosts.Load())
	}
}

func TestCIMDRejectsClientName(t *testing.T) {
	c := &Client{Store: Open(t.TempDir())}
	err := c.SignIn(context.Background(), Server{Name: "docs", URL: "https://mcp.example/mcp"}, Request{
		Settings: Settings{ClientRegistration: "cimd", ClientName: "other"},
	})
	if err == nil {
		t.Fatal("cimd with clientName succeeded")
	}
}

func TestCIMDRequiresServerSupport(t *testing.T) {
	f := &fakeAS{authMethods: []string{"client_secret_basic"}}
	f.start(t)
	client, _, server := f.client(t, "docs")
	err := client.SignIn(testCtx(t), server, Request{
		Settings: Settings{ClientRegistration: "cimd"},
		Prompt:   drive(t),
	})
	if err == nil {
		t.Fatal("cimd without public client support succeeded")
	}
	if f.registerPosts.Load() != 0 || f.tokenPosts.Load() != 0 {
		t.Fatalf("register %d token %d", f.registerPosts.Load(), f.tokenPosts.Load())
	}
}

func TestAuthServerMetadataURL(t *testing.T) {
	f := &fakeAS{prmServers: []string{"http://127.0.0.1:1"}}
	f.start(t)
	client, _, server := f.client(t, "docs")
	err := client.SignIn(testCtx(t), server, Request{
		Settings: Settings{AuthServerMetadataURL: f.srv.URL + "/configured-as"},
		Prompt:   drive(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.tokenPosts.Load() != 1 {
		t.Fatalf("token posts = %d", f.tokenPosts.Load())
	}
}

type fakeAS struct {
	srv            *httptest.Server
	tokenPosts     atomic.Int32
	registerPosts  atomic.Int32
	toolHits       atomic.Int32
	toolAuth       atomic.Value
	authScope      atomic.Value
	authClient     atomic.Value
	registerBody   atomic.Value
	mu             sync.Mutex
	grants         []string
	iss            string
	skipIss        bool
	omitIssuer     bool
	cimd           bool
	tokenBody      string
	prmServers     []string
	authMethods    []string
	blockRefresh   <-chan struct{}
	refreshStarted chan struct{}
	refreshOnce    sync.Once
}

func (f *fakeAS) start(t *testing.T) {
	t.Helper()
	if f.refreshStarted == nil {
		f.refreshStarted = make(chan struct{})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", f.prm)
	mux.HandleFunc("/.well-known/oauth-authorization-server", f.metadata)
	mux.HandleFunc("/configured-as", f.metadata)
	mux.HandleFunc("/register", f.register)
	mux.HandleFunc("/authorize", f.authorize)
	mux.HandleFunc("/token", f.token)
	mux.HandleFunc("/tools/call", f.tool)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
}

func (f *fakeAS) client(t *testing.T, name string) (*Client, *Store, Server) {
	t.Helper()
	st := Open(t.TempDir())
	return &Client{Store: st}, st, Server{Name: name, URL: f.srv.URL + "/mcp"}
}

func (f *fakeAS) issuer() string {
	if f.iss != "" {
		return f.iss
	}
	return f.srv.URL
}

func (f *fakeAS) prm(w http.ResponseWriter, _ *http.Request) {
	servers := f.prmServers
	if servers == nil {
		servers = []string{f.srv.URL}
	}
	enc, _ := json.Marshal(servers)
	fmt.Fprintf(w, `{"resource":%q,"authorization_servers":%s,"scopes_supported":["unrelated"]}`, f.srv.URL+"/mcp", enc)
}

func (f *fakeAS) metadata(w http.ResponseWriter, _ *http.Request) {
	methods := f.authMethods
	if methods == nil {
		methods = []string{"none"}
	}
	enc, _ := json.Marshal(methods)
	issuer := ""
	if !f.omitIssuer {
		issuer = fmt.Sprintf(`"issuer":%q,`, f.issuer())
	}
	cimd := ""
	if f.cimd {
		cimd = `,"client_id_metadata_document_supported":true`
	}
	fmt.Fprintf(w, `{%s"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q,"response_types_supported":["code"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":%s%s}`,
		issuer, f.srv.URL+"/authorize", f.srv.URL+"/token", f.srv.URL+"/register", enc, cimd)
}

func (f *fakeAS) register(w http.ResponseWriter, r *http.Request) {
	f.registerPosts.Add(1)
	b, _ := io.ReadAll(r.Body)
	f.registerBody.Store(string(b))
	_ = r.Body.Close()
	fmt.Fprint(w, `{"client_id":"dyn-client"}`)
}

func (f *fakeAS) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.authScope.Store(q.Get("scope"))
	f.authClient.Store(q.Get("client_id"))
	redir, err := http.NewRequest(http.MethodGet, q.Get("redirect_uri"), nil)
	if err != nil {
		http.Error(w, "bad redirect", http.StatusBadRequest)
		return
	}
	target := redir.URL
	v := target.Query()
	v.Set("code", "authcode")
	v.Set("state", q.Get("state"))
	if !f.skipIss {
		v.Set("iss", f.issuer())
	}
	target.RawQuery = v.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (f *fakeAS) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	grant := r.PostForm.Get("grant_type")
	f.mu.Lock()
	f.grants = append(f.grants, grant)
	f.mu.Unlock()
	if grant == "authorization_code" && r.PostForm.Get("code_verifier") == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if grant == "refresh_token" && f.blockRefresh != nil {
		f.tokenPosts.Add(1)
		f.refreshOnce.Do(func() { close(f.refreshStarted) })
		<-f.blockRefresh
		fmt.Fprint(w, `{"access_token":"rotated","token_type":"Bearer","expires_in":3600}`)
		return
	}
	f.tokenPosts.Add(1)
	body := f.tokenBody
	if body == "" {
		body = `{"access_token":"access-1","token_type":"Bearer","refresh_token":"refresh-1","expires_in":3600}`
	}
	w.Header().Set("content-type", "application/json")
	fmt.Fprint(w, body)
}

func (f *fakeAS) tool(w http.ResponseWriter, r *http.Request) {
	f.toolHits.Add(1)
	f.toolAuth.Store(r.Header.Get("Authorization"))
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeAS) scope() string      { return valueOf(f.authScope) }
func (f *fakeAS) clientID() string   { return valueOf(f.authClient) }
func (f *fakeAS) regBody() string    { return valueOf(f.registerBody) }
func (f *fakeAS) authHeader() string { return valueOf(f.toolAuth) }

func (f *fakeAS) grantList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.grants...)
	return out
}

func valueOf(v atomic.Value) string {
	got := v.Load()
	if got == nil {
		return ""
	}
	return got.(string)
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func drive(t *testing.T) Prompt {
	t.Helper()
	return Prompt{
		Open: func(string) {},
		Notify: func(ev auth.Event) {
			if ev.Type != auth.EventAuthURL || ev.URL == "" {
				t.Errorf("auth event = %+v", ev)
				return
			}
			res, err := http.Get(ev.URL)
			if err != nil {
				t.Errorf("authorize: %v", err)
				return
			}
			defer func() { _ = res.Body.Close() }()
			_, _ = io.Copy(io.Discard, res.Body)
		},
	}
}
