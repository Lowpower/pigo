package models

import (
	"strings"
	"testing"
)

func cliResolveCatalog() []Model {
	return []Model{
		{Provider: "zz-a", ID: "gpt-x"},
		{Provider: "zz-b", ID: "gpt-x"},
		{Provider: "zz-named", ID: "other"},
		{Provider: "zz-gate", ID: "zz-named/opus-x"},
		{Provider: "zz-gate2", ID: "zz-named/opus-x"},
		{Provider: "zz-only", ID: "solo"},
	}
}

func TestResolveCLIModelPrefersAuthenticatedProvider(t *testing.T) {
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:          "gpt-x",
		Authenticated: []string{"zz-b"},
		Models:        cliResolveCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Model.Provider != "zz-b" || got.Model.ID != "gpt-x" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveCLIModelAmbiguousWhenSeveralAuthenticated(t *testing.T) {
	_, err := ResolveCLIModel(CLIModelRequest{
		Text:          "gpt-x",
		Authenticated: []string{"zz-a", "zz-b"},
		Models:        cliResolveCatalog(),
	})
	if err == nil {
		t.Fatal("expected ambiguity")
	}
	want := `Model "gpt-x" is ambiguous across providers: zz-a/gpt-x, zz-b/gpt-x. More than one matching provider is authenticated. Use --provider or provider/model.`
	if err.Error() != want {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestResolveCLIModelAmbiguousWhenNoneAuthenticated(t *testing.T) {
	_, err := ResolveCLIModel(CLIModelRequest{
		Text:   "gpt-x",
		Models: cliResolveCatalog(),
	})
	if err == nil || !strings.Contains(err.Error(), "No matching provider is authenticated.") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "zz-a/gpt-x") || !strings.Contains(err.Error(), "zz-b/gpt-x") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveCLIModelInlineAPIKeyDoesNotBreakTies(t *testing.T) {
	_, err := ResolveCLIModel(CLIModelRequest{
		Text:          "gpt-x",
		Authenticated: []string{"zz-b"},
		InlineAPIKey:  true,
		Models:        cliResolveCatalog(),
	})
	if err == nil || !strings.Contains(err.Error(), "--api-key does not choose a provider.") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveCLIModelUniqueWithoutCredentials(t *testing.T) {
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:   "solo",
		Models: cliResolveCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Model.Provider != "zz-only" || got.Model.ID != "solo" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveCLIModelFullIDFallsBackToAuthenticatedProvider(t *testing.T) {
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:          "zz-named/opus-x",
		Authenticated: []string{"zz-gate2"},
		Models:        cliResolveCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Model.Provider != "zz-gate2" || got.Model.ID != "zz-named/opus-x" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveCLIModelNamedProviderWinsWhenUsable(t *testing.T) {
	list := append(cliResolveCatalog(), Model{Provider: "zz-named", ID: "opus-x"})
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:          "zz-named/opus-x",
		Authenticated: []string{"zz-named", "zz-gate", "zz-gate2"},
		Models:        list,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Model.Provider != "zz-named" || got.Model.ID != "opus-x" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveCLIModelUnusableNamedYieldsToSoleUsableRaw(t *testing.T) {
	list := append(cliResolveCatalog(), Model{Provider: "zz-named", ID: "opus-x"})
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:          "zz-named/opus-x",
		Authenticated: []string{"zz-gate"},
		Models:        list,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Model.Provider != "zz-gate" || got.Model.ID != "zz-named/opus-x" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveCLIModelInlineKeyKeepsNamedProvider(t *testing.T) {
	list := append(cliResolveCatalog(), Model{Provider: "zz-named", ID: "opus-x"})
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:          "zz-named/opus-x",
		Authenticated: []string{"zz-gate"},
		InlineAPIKey:  true,
		Models:        list,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Model.Provider != "zz-named" || got.Model.ID != "opus-x" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveCLIModelProviderConstraintIgnoresOtherProviders(t *testing.T) {
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:          "gpt-x",
		Provider:      "zz-a",
		Authenticated: []string{"zz-b"},
		Models:        cliResolveCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Model.Provider != "zz-a" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveCLIModelMissingStaysUnresolved(t *testing.T) {
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:   "not-in-catalog",
		Models: cliResolveCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Found {
		t.Fatalf("found %+v", got)
	}
}

func TestAmbiguousExactPatterns(t *testing.T) {
	list := cliResolveCatalog()
	got := AmbiguousExactPatterns([]string{"gpt-x", "gpt-x:high", "solo", "zz-*", "zz-a/gpt-x"}, list)
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if got[0] != `Pattern "gpt-x" matches several providers: zz-a, zz-b. Use provider/id to choose one.` {
		t.Fatalf("bare = %q", got[0])
	}
	if got[1] != `Pattern "gpt-x:high" matches several providers: zz-a, zz-b. Use provider/id to choose one.` {
		t.Fatalf("thinking = %q", got[1])
	}
}

func TestAmbiguousExactPatternsIgnoresSubstringExtras(t *testing.T) {
	list := []Model{
		{Provider: "openai", ID: "gpt-4o"},
		{Provider: "openai", ID: "gpt-4o-mini"},
		{Provider: "openrouter", ID: "openai/gpt-4o"},
	}
	if got := AmbiguousExactPatterns([]string{"gpt-4o"}, list); len(got) != 0 {
		t.Fatalf("substring should not warn: %v", got)
	}
}

func TestResolveCLIModelThinkingSuffixAndCase(t *testing.T) {
	got, err := ResolveCLIModel(CLIModelRequest{
		Text:          "GPT-X:high",
		Authenticated: []string{"zz-b"},
		Models:        cliResolveCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Model.Provider != "zz-b" || got.Model.ID != "gpt-x" {
		t.Fatalf("%+v", got)
	}
}
