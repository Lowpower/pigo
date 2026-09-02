package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestParseGitHubCopilotModelCatalog(t *testing.T) {
	raw := []byte(`{
  "data": [
    {"id":"keep","model_picker_enabled":true,"policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":true}}},
    {"id":"disabled","model_picker_enabled":true,"policy":{"state":"disabled"},"capabilities":{"supports":{"tool_calls":true}}},
    {"id":"no-picker","model_picker_enabled":false,"policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":true}}},
    {"id":"no-tools","model_picker_enabled":true,"policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":false}}}
  ]
}`)
	available, policy, err := parseGitHubCopilotModelCatalog(raw, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 1 || available[0] != "keep" {
		t.Fatalf("available = %v", available)
	}
	if len(policy) != 0 {
		t.Fatalf("policy = %v", policy)
	}
}

func TestParseGitHubCopilotModelCatalogPolicyFallback(t *testing.T) {
	raw := []byte(`{
  "data": [
    {"id":"policy-on","model_picker_enabled":false,"policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":true}}},
    {"id":"policy-off","model_picker_enabled":false,"policy":{"state":"disabled"},"capabilities":{"supports":{"tool_calls":true}}}
  ]
}`)
	available, _, err := parseGitHubCopilotModelCatalog(raw, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 1 || available[0] != "policy-on" {
		t.Fatalf("available = %v", available)
	}
}

func TestParseGitHubCopilotModelCatalogPolicyIDs(t *testing.T) {
	raw := []byte(`{
  "data": [
    {"id":"keep","model_picker_enabled":true,"policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":true}}},
    {"id":"need-enable","model_picker_enabled":true,"policy":{"state":"unconfigured"},"capabilities":{"supports":{"tool_calls":true}}},
    {"id":"unknown-unconfigured","model_picker_enabled":true,"policy":{"state":"unconfigured"},"capabilities":{"supports":{"tool_calls":true}}}
  ]
}`)
	available, policy, err := parseGitHubCopilotModelCatalog(raw, false, map[string]bool{"keep": true, "need-enable": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 1 || available[0] != "keep" {
		t.Fatalf("available = %v", available)
	}
	if len(policy) != 1 || policy[0] != "need-enable" {
		t.Fatalf("policy = %v", policy)
	}
}

func TestEnableCopilotModelsMergesAvailableIDs(t *testing.T) {
	t.Cleanup(func() { models.ClearAvailableModelIDs("github-copilot") })
	var policyPosts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/copilot_internal/v2/token":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"token":"copilot-tok","expires_at":1999999999}`))
		case r.URL.Path == "/models":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"id":"keep","model_picker_enabled":true,"policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":true}}},
				{"id":"need-enable","model_picker_enabled":true,"policy":{"state":"unconfigured"},"capabilities":{"supports":{"tool_calls":true}}}
			]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/policy"):
			policyPosts++
			if r.URL.Path != "/models/need-enable/policy" {
				t.Errorf("policy path = %s", r.URL.Path)
			}
			if r.Header.Get("openai-intent") != "chat-policy" {
				t.Errorf("openai-intent = %q", r.Header.Get("openai-intent"))
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldToken, oldModels := copilotTokenURL, copilotModelsBaseURL
	copilotTokenURL = srv.URL + "/copilot_internal/v2/token"
	copilotModelsBaseURL = srv.URL
	t.Cleanup(func() {
		copilotTokenURL = oldToken
		copilotModelsBaseURL = oldModels
	})

	got, err := refreshCopilotAccessOpts(t.Context(), "gh-token", "", copilotModelsOpts{
		EnablePolicies: true,
		KnownModelIDs:  map[string]bool{"keep": true, "need-enable": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if policyPosts != 1 {
		t.Fatalf("policy posts = %d", policyPosts)
	}
	ids := extraStringSlice(got, "availableModelIds")
	if len(ids) != 2 {
		t.Fatalf("available = %v", ids)
	}
}

func TestCopilotFilterModelsUsesAvailableIDs(t *testing.T) {
	t.Cleanup(func() { models.ClearAvailableModelIDs("github-copilot") })
	models.ClearAvailableModelIDs("github-copilot")
	spec, ok := models.LookupProvider("github-copilot")
	if !ok || spec.FilterModels == nil {
		t.Fatal("github-copilot FilterModels")
	}
	id := models.DefaultID("github-copilot")
	if !copilotCatalogHas(id) {
		t.Fatal("unfiltered catalog should keep the default model")
	}
	models.SetAvailableModelIDs("github-copilot", []string{"not-in-offline-catalog"})
	if copilotCatalogHas(id) {
		t.Fatal("oauth availableModelIds should drop models that are not allowlisted")
	}
	models.SetAvailableModelIDs("github-copilot", []string{id})
	if !copilotCatalogHas(id) {
		t.Fatal("allowlisted default model should remain")
	}
}

func TestRefreshCopilotAccessStoresAvailableModelIDs(t *testing.T) {
	t.Cleanup(func() { models.ClearAvailableModelIDs("github-copilot") })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/copilot_internal/v2/token":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"token":"copilot-tok","expires_at":1999999999}`))
		case "/models":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1","model_picker_enabled":true,"policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":true}}}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldToken, oldModels := copilotTokenURL, copilotModelsBaseURL
	copilotTokenURL = srv.URL + "/copilot_internal/v2/token"
	copilotModelsBaseURL = srv.URL
	t.Cleanup(func() {
		copilotTokenURL = oldToken
		copilotModelsBaseURL = oldModels
	})

	got, err := refreshCopilotAccess(t.Context(), "gh-token", "")
	if err != nil {
		t.Fatal(err)
	}
	ids := extraStringSlice(got, "availableModelIds")
	if len(ids) != 1 || ids[0] != "gpt-4.1" {
		t.Fatalf("extra = %#v cred=%+v", got.Extra, got)
	}
	if copilotCatalogHas("claude-fable-5") {
		t.Fatal("catalog should be filtered to oauth availableModelIds")
	}
}

func TestApplyEnvAppliesCopilotAvailableModelIDs(t *testing.T) {
	t.Cleanup(func() { models.ClearAvailableModelIDs("github-copilot") })
	dir := t.TempDir()
	s := Open(dir)
	if _, err := s.Modify("github-copilot", func(*Credential) (*Credential, error) {
		return &Credential{
			Type:   TypeOAuth,
			Access: "tok",
			Extra:  map[string]any{"availableModelIds": []any{"only-this"}},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	ApplyEnv(dir)
	if copilotCatalogHas(models.DefaultID("github-copilot")) {
		t.Fatal("stored availableModelIds should filter catalog")
	}
}

func extraStringSliceForTest(t *testing.T, raw string) {
	t.Helper()
	var c Credential
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if got := extraStringSlice(c, "availableModelIds"); len(got) != 1 || got[0] != "m1" {
		t.Fatalf("%v extra=%v", got, c.Extra)
	}
}

func TestExtraStringSliceFromJSON(t *testing.T) {
	extraStringSliceForTest(t, `{"type":"oauth","access":"a","availableModelIds":["m1"]}`)
}

func copilotCatalogHas(id string) bool {
	for _, m := range models.Catalog() {
		if m.Provider == "github-copilot" && m.ID == id {
			return true
		}
	}
	return false
}
