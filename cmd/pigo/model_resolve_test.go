package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/models"
)

func registerIssue350Provider(t *testing.T, id, modelID, env, key string) {
	t.Helper()
	models.RegisterProvider(models.ProviderSpec{
		ID:         id,
		Env:        []string{env},
		DefaultAPI: "openai-completions",
		Models:     []models.Model{{ID: modelID}},
	})
	auth.RegisterProvider(auth.Provider{
		ID:     id,
		APIKey: &auth.APIKeyHandler{Env: []string{env}},
	})
	t.Setenv(env, key)
	t.Cleanup(func() {
		models.UnregisterProvider(id)
		auth.UnregisterProvider(id)
		auth.ClearProcessAPIKey(id)
	})
}

func TestProviderRequiresModel(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--provider", "openai"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(err.Error(), "--provider requires --model (for example: --provider openai --model <model>)") {
		t.Fatalf("error = %q", err.Error())
	}
	if !strings.Contains(stderr.String(), "--provider openai --model <model>") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCLIModelPrefersAuthenticatedProvider(t *testing.T) {
	clearCatalogEnvs(t)
	registerIssue350Provider(t, "zz-issue350-a", "gpt-x", "PIGO_ISSUE350_A", "")
	registerIssue350Provider(t, "zz-issue350-b", "gpt-x", "PIGO_ISSUE350_B", "sk-b")
	out, errOut, err := runModelPrint(t, "--model", "gpt-x")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, errOut)
	}
	if !strings.Contains(out, "provider=zz-issue350-b model=gpt-x ") {
		t.Fatalf("out=%q stderr=%s", out, errOut)
	}
}

func TestCLIModelAmbiguousListsProviders(t *testing.T) {
	clearCatalogEnvs(t)
	registerIssue350Provider(t, "zz-issue350-a", "gpt-x", "PIGO_ISSUE350_A", "sk-a")
	registerIssue350Provider(t, "zz-issue350-b", "gpt-x", "PIGO_ISSUE350_B", "sk-b")
	_, stderr, err := runModelPrint(t, "--model", "gpt-x")
	if err == nil {
		t.Fatal("expected ambiguity")
	}
	for _, id := range []string{"zz-issue350-a/gpt-x", "zz-issue350-b/gpt-x"} {
		if !strings.Contains(err.Error(), id) || !strings.Contains(stderr, id) {
			t.Fatalf("err=%v stderr=%s", err, stderr)
		}
	}
}

func TestCLIModelFullIDUsesAuthenticatedProvider(t *testing.T) {
	clearCatalogEnvs(t)
	registerIssue350Provider(t, "zz-issue350-named", "other", "PIGO_ISSUE350_NAMED", "")
	registerIssue350Provider(t, "zz-issue350-gate", "zz-issue350-named/opus-x", "PIGO_ISSUE350_GATE", "")
	registerIssue350Provider(t, "zz-issue350-gate2", "zz-issue350-named/opus-x", "PIGO_ISSUE350_GATE2", "sk-g2")
	out, errOut, err := runModelPrint(t, "--model", "zz-issue350-named/opus-x")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, errOut)
	}
	if !strings.Contains(out, "provider=zz-issue350-gate2 model=zz-issue350-named/opus-x ") {
		t.Fatalf("out=%q stderr=%s", out, errOut)
	}
}

func TestProviderWithModelStaysOnThatProvider(t *testing.T) {
	clearCatalogEnvs(t)
	out, stderr, err := runModelPrint(t, "--provider", "openai", "--model", "gpt-4o")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(out, "provider=openai model=gpt-4o ") {
		t.Fatalf("out=%q stderr=%s", out, stderr)
	}
}

func TestCLIModelsWarnsDuplicateExactID(t *testing.T) {
	clearCatalogEnvs(t)
	registerIssue350Provider(t, "zz-issue350-a", "gpt-x", "PIGO_ISSUE350_A", "sk-a")
	registerIssue350Provider(t, "zz-issue350-b", "gpt-x", "PIGO_ISSUE350_B", "sk-b")
	_, stderr, err := runModelPrint(t, "--models", "gpt-x")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	want := `Warning: Pattern "gpt-x" matches several providers: zz-issue350-a, zz-issue350-b. Use provider/id to choose one.`
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr=%s", stderr)
	}
}

func runModelPrint(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	base := []string{
		"--print",
		"--offline",
		"--no-session",
		"--no-extensions",
		"--no-skills",
		"--no-tools",
		"--no-context-files",
		"--no-prompt-templates",
		"--no-themes",
		"--config-dir", t.TempDir(),
	}
	cmd.SetArgs(append(base, args...))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}
