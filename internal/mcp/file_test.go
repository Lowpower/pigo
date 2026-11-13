package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertPreservesUnknownFieldsAndLoads(t *testing.T) {
	agent := t.TempDir()
	path := UserPath(agent)
	writeJSON(t, path, `{
		"mcpServers": {
			"demo": {"command": "old", "auth": {"provider": "demo"}, "oauth": {"scope": "keep"}}
		}
	}`)
	err := Upsert(path, UpsertSpec{
		Name:              "demo",
		URL:               "http://127.0.0.1:9/mcp",
		Description:       "docs",
		Exposure:          "direct",
		Headers:           map[string]string{"X-Test": "1"},
		OAuthClientID:     "cid",
		OAuthClientSecret: "sec",
		OAuthClientName:   "pigo",
		OAuthCallbackPort: 8787,
		BearerTokenEnvVar: "MCP_TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, diags := Load(agent, t.TempDir(), false)
	if len(diags) != 0 {
		t.Fatalf("diags=%v", diags)
	}
	demo := serverNamed(got, "demo")
	if demo.Config.URL != "http://127.0.0.1:9/mcp" || demo.Config.Command != "" {
		t.Fatalf("loaded=%+v", demo.Config)
	}
	if demo.Config.Description != "docs" || demo.Config.ExposureValue() != "direct" {
		t.Fatalf("meta=%+v", demo.Config)
	}
	if demo.Config.Headers["X-Test"] != "1" || demo.Config.BearerTokenEnvVar != "MCP_TOKEN" {
		t.Fatalf("headers=%v bearer=%s", demo.Config.Headers, demo.Config.BearerTokenEnvVar)
	}
	raw := string(demo.Config.Raw)
	if !strings.Contains(raw, `"provider"`) || !strings.Contains(raw, `"scope"`) {
		t.Fatalf("dropped unknown fields: %s", raw)
	}
	if !strings.Contains(raw, `"clientId"`) || !strings.Contains(raw, "8787") {
		t.Fatalf("oauth flags missing: %s", raw)
	}

	if err := Remove(path, "demo"); err != nil {
		t.Fatal(err)
	}
	left, _ := Load(agent, t.TempDir(), false)
	if serverNamed(left, "demo").Name != "" {
		t.Fatalf("still present: %+v", left)
	}
}

func TestSetEnabledKeepsOtherFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	writeJSON(t, path, `{"mcpServers":{"demo":{"command":"srv","env":{"A":"b"}}}}`)
	if err := SetEnabled(path, "demo", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enabled": false`) || !strings.Contains(string(b), `"A"`) {
		t.Fatalf("file=%s", b)
	}
	if err := SetEnabled(path, "missing", true); err == nil {
		t.Fatal("missing server should fail")
	}
}

func TestLocalPathRefusesUntrustedProject(t *testing.T) {
	_, err := ConfigPath(t.TempDir(), t.TempDir(), true, false)
	if err == nil {
		t.Fatal("untrusted project write was allowed")
	}
	path, err := ConfigPath(t.TempDir(), t.TempDir(), false, false)
	if err != nil || !strings.HasSuffix(path, "mcp.json") {
		t.Fatalf("user path=%s err=%v", path, err)
	}
}
