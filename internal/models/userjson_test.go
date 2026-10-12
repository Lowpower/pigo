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

func TestLoadUserJSONReadsSessionAffinityFormat(t *testing.T) {
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("affinity-json")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "affinity-json": {
      "api": "openai-completions",
      "baseUrl": "https://example.invalid/v1",
      "models": [
        {"id": "m1", "compat": {"sessionAffinityFormat": "openrouter"}},
        {"id": "m2"}
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
	m1, ok := Lookup("affinity-json", "m1")
	if !ok || m1.Compat == nil || m1.Compat.SessionAffinityFormat != "openrouter" {
		t.Fatalf("m1 = %+v ok=%v", m1, ok)
	}
	m2, ok := Lookup("affinity-json", "m2")
	if !ok {
		t.Fatal("m2 missing")
	}
	if m2.Compat != nil && m2.Compat.SessionAffinityFormat != "" {
		t.Fatalf("m2 compat = %+v, want empty sessionAffinityFormat", m2.Compat)
	}
}

func TestLoadUserJSONReadsSupportsMaxOutputTokens(t *testing.T) {
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("maxout-json")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "maxout-json": {
      "api": "openai-responses",
      "baseUrl": "https://example.invalid/v1",
      "models": [
        {"id": "m1", "compat": {"supportsMaxOutputTokens": false}},
        {"id": "m2", "compat": {"supportsMaxOutputTokens": true}},
        {"id": "m3"}
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
	m1, ok := Lookup("maxout-json", "m1")
	if !ok || m1.Compat == nil || m1.Compat.SupportsMaxOutputTokens == nil || *m1.Compat.SupportsMaxOutputTokens {
		t.Fatalf("m1 = %+v ok=%v, want supportsMaxOutputTokens=false", m1, ok)
	}
	m2, ok := Lookup("maxout-json", "m2")
	if !ok || m2.Compat == nil || m2.Compat.SupportsMaxOutputTokens == nil || !*m2.Compat.SupportsMaxOutputTokens {
		t.Fatalf("m2 = %+v ok=%v, want supportsMaxOutputTokens=true", m2, ok)
	}
	m3, ok := Lookup("maxout-json", "m3")
	if !ok {
		t.Fatal("m3 missing")
	}
	if m3.Compat != nil && m3.Compat.SupportsMaxOutputTokens != nil {
		t.Fatalf("m3 compat = %+v, want unset supportsMaxOutputTokens", m3.Compat)
	}
}

func TestLoadUserJSONReadsSendSessionAffinityHeaders(t *testing.T) {
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("aff-json")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "aff-json": {
      "api": "anthropic-messages",
      "baseUrl": "https://example.invalid/v1",
      "models": [
        {"id": "m1", "compat": {"sendSessionAffinityHeaders": true}},
        {"id": "m2"}
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
	m1, ok := Lookup("aff-json", "m1")
	if !ok || m1.Compat == nil || !m1.Compat.SendSessionAffinityHeaders {
		t.Fatalf("m1 = %+v ok=%v, want sendSessionAffinityHeaders=true", m1, ok)
	}
	m2, ok := Lookup("aff-json", "m2")
	if !ok {
		t.Fatal("m2 missing")
	}
	if m2.Compat != nil && m2.Compat.SendSessionAffinityHeaders {
		t.Fatalf("m2 compat = %+v, want sendSessionAffinityHeaders unset", m2.Compat)
	}
}

func TestLoadUserJSONMergesImageResize(t *testing.T) {
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
      "models": [
        {"id": "gpt-4o", "inputLimits": {"images": {"resize": {"maxWidth": 1568, "jpegQuality": 75}}}}
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
	if !ok {
		t.Fatal("gpt-4o missing")
	}
	resize := m.ImageResize()
	if resize == nil || resize.MaxWidth == nil || *resize.MaxWidth != 1568 {
		t.Fatalf("maxWidth = %+v", resize)
	}
	if resize.JPEGQuality == nil || *resize.JPEGQuality != 75 {
		t.Fatalf("jpegQuality = %+v", resize)
	}
	if resize.MaxHeight != nil || resize.MaxBytes != nil {
		t.Fatalf("unset resize fields = %+v", resize)
	}
	if m.MaxTokens != 16384 {
		t.Fatalf("maxTokens = %d", m.MaxTokens)
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
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("models.json mode=%o", st.Mode().Perm())
	}
}

func TestLoadUserJSONSamplingParams(t *testing.T) {
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("samp-json")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "samp-json": {
      "api": "openai-completions",
      "baseUrl": "http://127.0.0.1:8080/v1",
      "models": [
        {"id": "local", "samplingParams": {"temperature": 1, "top_k": 0, "min_p": 0.05, "vendor_flag": false}},
        {"id": "plain", "samplingParams": {}}
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
	m, ok := Lookup("samp-json", "local")
	if !ok {
		t.Fatal("local missing")
	}
	if m.SamplingParams["temperature"] != float64(1) || m.SamplingParams["top_k"] != float64(0) {
		t.Fatalf("params = %#v", m.SamplingParams)
	}
	if m.SamplingParams["min_p"] != 0.05 || m.SamplingParams["vendor_flag"] != false {
		t.Fatalf("unknown/zero params = %#v", m.SamplingParams)
	}
	plain, ok := Lookup("samp-json", "plain")
	if !ok || len(plain.SamplingParams) != 0 {
		t.Fatalf("empty object = %#v ok=%v", plain.SamplingParams, ok)
	}
}

func TestLoadUserJSONForceAdaptiveThinkingFalse(t *testing.T) {
	on := true
	RegisterProvider(ProviderSpec{
		ID: "adapt-json", DefaultAPI: "anthropic-messages", DefaultID: "m",
		Models: []Model{{
			Provider: "adapt-json", ID: "m",
			Compat: &Compat{ForceAdaptiveThinking: &on, ThinkingFormat: "keep"},
		}},
	})
	t.Cleanup(func() {
		ClearOverlays()
		UnregisterProvider("adapt-json")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `{
  "providers": {
    "adapt-json": {
      "models": [
        {"id": "m", "compat": {"forceAdaptiveThinking": false}}
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
	m, ok := Lookup("adapt-json", "m")
	if !ok || m.Compat == nil || m.Compat.ForceAdaptiveThinking == nil || *m.Compat.ForceAdaptiveThinking {
		t.Fatalf("forceAdaptiveThinking = %+v ok=%v", m.Compat, ok)
	}
	if m.Compat.ThinkingFormat != "keep" {
		t.Fatalf("thinking format lost: %+v", m.Compat)
	}
}

func TestOpenAICatalogUsesResponsesAPI(t *testing.T) {
	if APIFor("openai", "gpt-4o") != "openai-responses" {
		t.Fatalf("openai gpt-4o api = %q", APIFor("openai", "gpt-4o"))
	}
}

func TestLoadUserJSONStripsBOM(t *testing.T) {
	t.Cleanup(ClearOverlays)
	path := filepath.Join(t.TempDir(), "models.json")
	body := "\ufeff" + `{"providers":{"bomprov":{"baseUrl":"https://example.test/v1","api":"openai-completions","models":[{"id":"m1"}]}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadUserJSON(path); err != nil {
		t.Fatal(err)
	}
	m, ok := Lookup("bomprov", "m1")
	if !ok || m.BaseURL != "https://example.test/v1" {
		t.Fatalf("model = %+v ok=%v", m, ok)
	}
}
