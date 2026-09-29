package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestRegisterUserJSONAuthFromModelsJSONKey(t *testing.T) {
	t.Cleanup(func() {
		UnregisterProvider("co-models-key")
		models.ClearOverlays()
		models.UnregisterProvider("co-models-key")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "co-models-key": {
      "baseUrl": "https://example.invalid/v1",
      "api": "openai-completions",
      "apiKey": "sk-from-models",
      "models": [{"id": "GLM-5.3"}]
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := models.LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	RegisterUserJSON()
	p, ok := Lookup("co-models-key")
	if !ok || p.APIKey == nil {
		t.Fatalf("auth lookup co-models-key: %+v ok=%v", p, ok)
	}
	s := Open(dir)
	chk := CheckAuth(s, "co-models-key")
	if chk == nil || chk.Type != TypeAPIKey {
		t.Fatalf("check = %+v", chk)
	}
	found := false
	for _, id := range AuthenticatedIDs(s) {
		if id == "co-models-key" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("AuthenticatedIDs missing co-models-key: %v", AuthenticatedIDs(s))
	}
	res, err := Resolve(context.Background(), s, p, ResolveOpts{})
	if err != nil || res == nil || res.Auth.APIKey != "sk-from-models" {
		t.Fatalf("resolve = %+v err=%v", res, err)
	}
}

func TestRegisterUserJSONAuthFromAuthJSON(t *testing.T) {
	t.Cleanup(func() {
		UnregisterProvider("co-auth-json")
		models.ClearOverlays()
		models.UnregisterProvider("co-auth-json")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "co-auth-json": {
      "baseUrl": "https://example.invalid/v1",
      "api": "openai-completions",
      "models": [{"id": "GLM-5.3"}]
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := models.LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	if err := SetAPIKey(dir, "co-auth-json", "sk-from-auth"); err != nil {
		t.Fatal(err)
	}
	RegisterUserJSON()
	p, ok := Lookup("co-auth-json")
	if !ok {
		t.Fatal("co-auth-json not in auth registry")
	}
	s := Open(dir)
	chk := CheckAuth(s, "co-auth-json")
	if chk == nil {
		t.Fatal("CheckAuth nil with auth.json key")
	}
	res, err := Resolve(context.Background(), s, p, ResolveOpts{})
	if err != nil || res == nil || res.Auth.APIKey != "sk-from-auth" {
		t.Fatalf("resolve = %+v err=%v", res, err)
	}
}

func TestRegisterUserJSONDoesNotReplaceBuiltin(t *testing.T) {
	orig, ok := Lookup("openai")
	if !ok {
		t.Fatal("openai auth missing")
	}
	t.Cleanup(func() {
		RegisterProvider(orig)
		models.ClearOverlays()
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "openai": {
      "baseUrl": "https://proxy.example/v1",
      "api": "openai-completions",
      "apiKey": "sk-should-not-replace",
      "models": [{"id": "custom-model"}]
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := models.LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	RegisterUserJSON()
	p, ok := Lookup("openai")
	if !ok {
		t.Fatal("openai auth dropped")
	}
	if p.APIKey == nil || len(p.APIKey.Env) == 0 || p.APIKey.Env[0] != "OPENAI_API_KEY" {
		t.Fatalf("openai auth replaced: %+v", p.APIKey)
	}
}

func TestRegisterUserJSONRadiusOAuth(t *testing.T) {
	t.Cleanup(func() {
		UnregisterProvider("radius-dev")
		models.ClearOverlays()
		models.UnregisterProvider("radius-dev")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "radius-dev": {
      "name": "Radius (dev)",
      "baseUrl": "http://127.0.0.1:8788",
      "oauth": "radius"
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := models.LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	RegisterUserJSON()
	p, ok := Lookup("radius-dev")
	if !ok {
		t.Fatal("radius-dev auth missing")
	}
	r, isRadius := p.OAuth.(radiusOAuth)
	if !isRadius || r.gateway != "http://127.0.0.1:8788" || r.name != "Radius (dev)" {
		t.Fatalf("oauth = %#v radius=%v", p.OAuth, isRadius)
	}
	if p.APIKey != nil {
		t.Fatal("radius oauth provider registered as api key")
	}
	got, err := p.OAuth.ToAuth(Credential{Access: "tok"})
	if err != nil || got.APIKey != "tok" || got.BaseURL != "" {
		t.Fatalf("toAuth = %+v err=%v", got, err)
	}
	if _, ok := models.Lookup("radius-dev", "balanced"); ok {
		t.Fatal("custom gateway inherited the public catalog")
	}
}
