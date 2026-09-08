package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserJSONOverridesAPIAndBaseURL(t *testing.T) {
	orig, _ := LookupProvider("openai")
	t.Cleanup(func() {
		ClearOverlays()
		RegisterProvider(orig)
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "openai": {
      "baseUrl": "https://proxy.example/v1",
      "api": "openai-completions",
      "models": [
        {"id": "gpt-4o", "api": "openai-completions"},
        {"id": "custom-model", "api": "openai-responses"}
      ]
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	m, ok := Lookup("openai", "gpt-4o")
	if !ok || m.API != "openai-completions" {
		t.Fatalf("gpt-4o = %+v ok=%v, want openai-completions", m, ok)
	}
	if m.BaseURL != "https://proxy.example/v1" {
		t.Fatalf("baseUrl = %q", m.BaseURL)
	}
	custom, ok := Lookup("openai", "custom-model")
	if !ok || custom.API != "openai-responses" {
		t.Fatalf("custom-model = %+v ok=%v", custom, ok)
	}
	if APIFor("openai", "unknown") != "openai-completions" {
		t.Fatalf("provider default api = %q", APIFor("openai", "unknown"))
	}
}

func TestLoadUserJSONRegistersUnknownProvider(t *testing.T) {
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("my-proxy")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{"providers":{"my-proxy":{"api":"openai-responses","baseUrl":"https://x","models":[{"id":"m1"}]}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	m, ok := Lookup("my-proxy", "m1")
	if !ok || m.API != "openai-responses" || m.BaseURL != "https://x" {
		t.Fatalf("got %+v ok=%v", m, ok)
	}
}

func TestLoadUserJSONExposesAPIKey(t *testing.T) {
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("co-userjson")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "co-userjson": {
      "baseUrl": "https://example.invalid/v1",
      "api": "openai-completions",
      "apiKey": "sk-test",
      "models": [{"id": "GLM-5.3", "name": "GLM-5.3"}]
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	var found UserJSONProvider
	for _, p := range UserJSONProviders() {
		if p.ID == "co-userjson" {
			found = p
			break
		}
	}
	if found.ID == "" {
		t.Fatal("co-userjson missing from UserJSONProviders")
	}
	if found.APIKey != "sk-test" {
		t.Fatalf("apiKey = %q", found.APIKey)
	}
	if found.API != "openai-completions" || found.BaseURL != "https://example.invalid/v1" {
		t.Fatalf("got %+v", found)
	}
}

func TestOpenAICatalogUsesResponsesAPI(t *testing.T) {
	if APIFor("openai", "gpt-4o") != "openai-responses" {
		t.Fatalf("openai gpt-4o api = %q", APIFor("openai", "gpt-4o"))
	}
}
