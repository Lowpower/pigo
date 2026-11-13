package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/mcp"
	"github.com/Lowpower/pigo/internal/mcp/oauth"
)

func TestMCPAddJSONLoadsAndRemoveKeepsCredentials(t *testing.T) {
	agent := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", agent)
	cwd := t.TempDir()
	t.Chdir(cwd)

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"mcp", "add", "docs",
		"--description", "Docs",
		"--exposure", "direct",
		"--oauth-client-id", "cid",
		"--oauth-client-name", "pigo",
		"--oauth-callback-port", "8787",
		"--bearer-token-env-var", "MCP_TOKEN",
		"--header", "X-Test=1",
		"--url", "https://mcp.example/mcp",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	got, diags := mcp.Load(agent, cwd, true)
	if len(diags) != 0 || len(got) != 1 {
		t.Fatalf("servers=%+v diags=%v", got, diags)
	}
	docs := got[0]
	if docs.Name != "docs" || docs.Config.URL != "https://mcp.example/mcp" || docs.Config.ExposureValue() != "direct" {
		t.Fatalf("loaded=%+v", docs)
	}
	if docs.Config.BearerTokenEnvVar != "MCP_TOKEN" || docs.Config.Headers["X-Test"] != "1" {
		t.Fatalf("headers=%v bearer=%s", docs.Config.Headers, docs.Config.BearerTokenEnvVar)
	}
	settings, err := mcp.OAuthSettings(docs.Config)
	if err != nil || settings.ClientID != "cid" || settings.CallbackPort != 8787 || settings.ClientName != "pigo" {
		t.Fatalf("oauth=%+v err=%v raw=%s", settings, err, docs.Config.Raw)
	}

	store := oauth.Open(agent)
	if err := store.Save("docs", docs.Config.URL, oauth.Record{Tokens: &oauth.Tokens{AccessToken: "keep"}}); err != nil {
		t.Fatal(err)
	}
	root = newRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"mcp", "remove", "docs"})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	left, _ := mcp.Load(agent, cwd, true)
	if len(left) != 0 {
		t.Fatalf("still loaded: %+v", left)
	}
	if rec, ok, err := store.Load("docs", "https://mcp.example/mcp"); err != nil || !ok || rec.Tokens.AccessToken != "keep" {
		t.Fatalf("credentials removed: %+v ok=%v err=%v", rec.Tokens, ok, err)
	}
}

func TestMCPAddLocalRefusesUntrustedProject(t *testing.T) {
	agent := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", agent)
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.MkdirAll(filepath.Join(cwd, ".pigo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pigo", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"mcp", "add", "--local", "demo", "--", "demo-server"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
}

func TestMCPListJSON(t *testing.T) {
	agent := t.TempDir()
	t.Setenv("PIGO_CODING_AGENT_DIR", agent)
	cwd := t.TempDir()
	t.Chdir(cwd)
	body := `{"mcpServers":{"off":{"command":"not-started","enabled":false}}}`
	if err := os.WriteFile(filepath.Join(agent, "mcp.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"mcp", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if strings.Contains(out.String(), "resourceTemplates") {
		t.Fatalf("counts leaked: %s", out.String())
	}
	var entries []mcp.ListEntry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatal(err, out.String())
	}
	if len(entries) != 1 || entries[0].Name != "off" || entries[0].Status != "disabled" || entries[0].Resources {
		t.Fatalf("entries=%+v\n%s", entries, out.String())
	}
	if strings.Contains(out.String(), `"resources"`) {
		t.Fatalf("disabled server reported resources: %s", out.String())
	}
}

func TestNoMCPFlagIsKnown(t *testing.T) {
	rest, unknown := peelUnknownFlags([]string{"--no-mcp", "-p", "hi"})
	if len(unknown) != 0 || strings.Join(rest, ",") != "--no-mcp,-p,hi" {
		t.Fatalf("rest=%v unknown=%v", rest, unknown)
	}
	if newRootCmd().Flags().Lookup("no-mcp") == nil {
		t.Fatal("missing --no-mcp")
	}
}
