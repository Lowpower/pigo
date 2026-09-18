package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/models"
)

type credOAuth struct {
	cred Credential
}

func (credOAuth) Name() string                            { return "stub" }
func (credOAuth) LoginLabel() string                      { return "" }
func (credOAuth) IsSubscription() bool                    { return false }
func (s credOAuth) Login(Interaction) (Credential, error) { return s.cred, nil }
func (credOAuth) Refresh(context.Context, Credential) (Credential, error) {
	return Credential{}, nil
}
func (credOAuth) ToAuth(Credential) (ModelAuth, error) { return ModelAuth{}, nil }

func replaceRadiusOAuth(t *testing.T, oauth OAuth) {
	t.Helper()
	orig, ok := Lookup("radius")
	if !ok {
		t.Fatal("radius provider missing")
	}
	t.Cleanup(func() { RegisterProvider(orig) })
	RegisterProvider(Provider{ID: "radius", OAuth: oauth})
}

func TestLoginWaitsForRadiusCatalog(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.ClearOverlays()
	replaceRadiusOAuth(t, credOAuth{cred: Credential{Type: TypeOAuth, Access: "oauth-tok"}})

	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseUrl": "https://messages.example",
			"models":  []map[string]any{{"id": "balanced"}},
		})
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)
	t.Setenv("RADIUS_API_KEY", "")

	dir := t.TempDir()
	if err := Login(t.Context(), Open(dir), "radius", TypeOAuth, Interaction{}); err != nil {
		t.Fatal(err)
	}
	if gotAuth.Load() != "Bearer oauth-tok" {
		t.Fatalf("authorization = %v", gotAuth.Load())
	}
	if _, ok := models.Lookup("radius", "balanced"); !ok {
		t.Fatal("catalog not loaded after radius login")
	}
	c, ok, err := Open(dir).Read("radius")
	if err != nil || !ok || c.Access != "oauth-tok" {
		t.Fatalf("persisted cred = %+v ok=%v err=%v", c, ok, err)
	}
}

func TestLoginRadiusCatalogTimeoutStillSucceeds(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.ClearOverlays()
	replaceRadiusOAuth(t, credOAuth{cred: Credential{Type: TypeOAuth, Access: "oauth-tok"}})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baseUrl": "https://messages.example",
			"models":  []map[string]any{},
		})
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)

	var notes []Event
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Millisecond)
	defer cancel()
	dir := t.TempDir()
	if err := Login(ctx, Open(dir), "radius", TypeOAuth, Interaction{
		Notify: func(ev Event) { notes = append(notes, ev) },
	}); err != nil {
		t.Fatal(err)
	}
	c, ok, err := Open(dir).Read("radius")
	if err != nil || !ok || c.Access != "oauth-tok" {
		t.Fatalf("login should persist cred on timeout: %+v ok=%v err=%v", c, ok, err)
	}
	found := false
	for _, ev := range notes {
		if ev.Type == EventInfo && ev.Message != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected timeout info notify, got %#v", notes)
	}
}

func TestLoginNonRadiusDoesNotWaitForCatalog(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.ClearOverlays()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)

	id := "login-wait-other"
	RegisterProvider(Provider{
		ID:    id,
		OAuth: credOAuth{cred: Credential{Type: TypeOAuth, Access: "other-tok"}},
	})
	t.Cleanup(func() { UnregisterProvider(id) })

	dir := t.TempDir()
	if err := Login(t.Context(), Open(dir), id, TypeOAuth, Interaction{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatalf("non-radius login hit catalog %d times", hits.Load())
	}
}

func TestLoginRadiusAPIKeyDoesNotWaitForCatalog(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.ClearOverlays()
	orig, ok := Lookup("radius")
	if !ok {
		t.Fatal("radius provider missing")
	}
	t.Cleanup(func() { RegisterProvider(orig) })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()
	t.Setenv("RADIUS_GATEWAY", srv.URL)

	RegisterProvider(Provider{
		ID: "radius",
		APIKey: &APIKeyHandler{
			Name: "Radius API key",
			Login: func(Interaction) (Credential, error) {
				return Credential{Type: TypeAPIKey, Key: "rk"}, nil
			},
		},
	})
	dir := t.TempDir()
	if err := Login(t.Context(), Open(dir), "radius", TypeAPIKey, Interaction{}); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatalf("radius api_key login hit catalog %d times", hits.Load())
	}
}
