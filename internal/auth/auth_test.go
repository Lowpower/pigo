package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTopLevelRoundTripAndLegacyUnwrap(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  "providers": {
    "anthropic": { "type": "api_key", "key": "sk-legacy" }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(dir)
	c, ok, err := s.Read("anthropic")
	if err != nil || !ok || c.Key != "sk-legacy" {
		t.Fatalf("read legacy: ok=%v err=%v cred=%+v", ok, err, c)
	}
	if _, err := s.Modify("openai", func(*Credential) (*Credential, error) {
		return &Credential{Type: TypeAPIKey, Key: "sk-new"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["providers"]; ok {
		t.Fatalf("wrote wrapped file: %s", raw)
	}
	if _, ok := m["anthropic"]; !ok {
		t.Fatalf("missing anthropic: %s", raw)
	}
}

func TestOAuthExtraRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	cred := Credential{Type: TypeOAuth, Access: "a", Refresh: "r", Expires: 1}.withExtra("accountId", "acct-1")
	if _, err := s.Modify("openai-codex", func(*Credential) (*Credential, error) {
		return &cred, nil
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Read("openai-codex")
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if got.extraString("accountId") != "acct-1" {
		t.Fatalf("extra lost: %+v extra=%v", got, got.Extra)
	}
}

func TestMigrateOAuthJSONAndAPIKeys(t *testing.T) {
	dir := t.TempDir()
	oauth := `{"anthropic":{"access":"tok","refresh":"ref","expires":111}}`
	settings := `{"theme":"dark","apiKeys":{"openai":"sk-from-settings"}}`
	if err := os.WriteFile(filepath.Join(dir, "oauth.json"), []byte(oauth), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Migrate(dir)
	if len(got) != 2 {
		t.Fatalf("migrated %v", got)
	}
	s := Open(dir)
	c, ok, _ := s.Read("anthropic")
	if !ok || c.Type != TypeOAuth || c.Access != "tok" {
		t.Fatalf("oauth migrate: %+v", c)
	}
	c2, ok, _ := s.Read("openai")
	if !ok || c2.Key != "sk-from-settings" {
		t.Fatalf("api key migrate: %+v", c2)
	}
	if _, err := os.Stat(filepath.Join(dir, "oauth.json")); !os.IsNotExist(err) {
		t.Fatalf("oauth.json should be renamed")
	}
	if _, err := os.Stat(filepath.Join(dir, "oauth.json.migrated")); err != nil {
		t.Fatal(err)
	}
	if Migrate(dir) != nil {
		t.Fatal("second migrate should no-op when auth.json exists")
	}
}

func TestResolveConfigValue(t *testing.T) {
	t.Setenv("PIGO_AUTH_TEST_KEY", "from-env")
	if got := ResolveConfigValue("$PIGO_AUTH_TEST_KEY", nil); got != "from-env" {
		t.Fatalf("env: %q", got)
	}
	if got := ResolveConfigValue("${PIGO_AUTH_TEST_KEY}-x", nil); got != "from-env-x" {
		t.Fatalf("template: %q", got)
	}
	if got := ResolveConfigValue("$$keep", nil); got != "$keep" {
		t.Fatalf("escape: %q", got)
	}
	ClearConfigValueCache()
	if got := ResolveConfigValue("!printf %s hi-cmd", nil); got != "hi-cmd" {
		t.Fatalf("command: %q", got)
	}
}

func TestModifyRefreshOnce(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	_, err := s.Modify("anthropic", func(*Credential) (*Credential, error) {
		return &Credential{Type: TypeOAuth, Access: "old", Refresh: "r", Expires: time.Now().UnixMilli() - 1}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	p := Provider{
		ID: "anthropic",
		OAuth: staticOAuth{
			refresh: func(_ context.Context, c Credential) (Credential, error) {
				n.Add(1)
				time.Sleep(20 * time.Millisecond)
				c.Access = "new"
				c.Expires = time.Now().UnixMilli() + 60*60*1000
				return c, nil
			},
			toAuth: func(c Credential) (ModelAuth, error) {
				return ModelAuth{APIKey: c.Access}, nil
			},
		},
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := Resolve(context.Background(), s, p, ResolveOpts{})
			if err != nil {
				t.Errorf("resolve: %v", err)
				return
			}
			if res == nil || res.Auth.APIKey != "new" {
				t.Errorf("got %+v", res)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 1 {
		t.Fatalf("refresh count %d want 1", n.Load())
	}
}

func TestPrintAPIKeySkipsOAuth(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	_, _ = s.Modify("anthropic", func(*Credential) (*Credential, error) {
		return &Credential{Type: TypeOAuth, Access: "oauth-tok", Refresh: "r", Expires: time.Now().UnixMilli() + 3600_000}, nil
	})
	c, ok, _ := s.Read("anthropic")
	if !ok || c.Type != TypeOAuth {
		t.Fatal(c)
	}
	p, _ := Lookup("anthropic")
	res, err := Resolve(context.Background(), s, p, ResolveOpts{})
	if err != nil || res == nil {
		t.Fatal(err, res)
	}
	infos, _ := s.List()
	var typ string
	for _, i := range infos {
		if i.ProviderID == "anthropic" {
			typ = i.Type
		}
	}
	if typ != TypeOAuth {
		t.Fatalf("list type %s", typ)
	}
}

func TestCheckNoRefresh(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	_, _ = s.Modify("anthropic", func(*Credential) (*Credential, error) {
		return &Credential{Type: TypeOAuth, Access: "tok", Refresh: "r", Expires: 1}, nil
	})
	ro := ReadOnly(dir)
	if chk := CheckAuth(ro, "anthropic"); chk == nil || chk.Type != TypeOAuth {
		t.Fatalf("check: %+v", chk)
	}
	c, ok, _ := ro.Read("anthropic")
	if !ok || c.Access != "tok" {
		t.Fatal(c)
	}
	if _, err := ro.Modify("anthropic", func(*Credential) (*Credential, error) {
		return &Credential{Type: TypeOAuth}, nil
	}); err == nil {
		t.Fatal("expected read-only modify error")
	}
}

type staticOAuth struct {
	refresh func(context.Context, Credential) (Credential, error)
	toAuth  func(Credential) (ModelAuth, error)
}

func (staticOAuth) Name() string         { return "static" }
func (staticOAuth) LoginLabel() string   { return "" }
func (staticOAuth) IsSubscription() bool { return false }
func (staticOAuth) Login(Interaction) (Credential, error) {
	return Credential{}, nil
}
func (s staticOAuth) Refresh(ctx context.Context, c Credential) (Credential, error) {
	return s.refresh(ctx, c)
}
func (s staticOAuth) ToAuth(c Credential) (ModelAuth, error) { return s.toAuth(c) }

func TestCheckAuthGoogleEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "g-key")
	s := Open(t.TempDir())
	chk := CheckAuth(s, "google")
	if chk == nil || chk.Source != "GEMINI_API_KEY" {
		t.Fatalf("check = %+v", chk)
	}
}

func TestCheckAuthGroqFromEnv(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "g")
	s := Open(t.TempDir())
	chk := CheckAuth(s, "groq")
	if chk == nil || chk.Source != "GROQ_API_KEY" {
		t.Fatalf("check = %+v", chk)
	}
	ids := AuthenticatedIDs(s)
	found := false
	for _, id := range ids {
		if id == "groq" {
			found = true
		}
	}
	if !found {
		t.Fatalf("AuthenticatedIDs missing groq: %v", ids)
	}
}

func TestCheckAuthKimiAPIKeyEnv(t *testing.T) {
	t.Setenv("KIMI_API_KEY", "k")
	s := Open(t.TempDir())
	chk := CheckAuth(s, "kimi-coding")
	if chk == nil || chk.Source != "KIMI_API_KEY" {
		t.Fatalf("check = %+v", chk)
	}
}

func TestCheckAuthBedrockAmbient(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	s := Open(t.TempDir())
	chk := CheckAuth(s, "amazon-bedrock")
	if chk == nil || chk.Source != "AWS_ACCESS_KEY_ID" {
		t.Fatalf("check = %+v", chk)
	}
}

func TestCheckAuthBedrockNeedsSecret(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", "")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	s := Open(t.TempDir())
	if chk := CheckAuth(s, "amazon-bedrock"); chk != nil {
		t.Fatalf("access key alone should not auth: %+v", chk)
	}
}
