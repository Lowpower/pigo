package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/mcp/oauth"
)

func TestDisplayTitle(t *testing.T) {
	if got := DisplayTitle("mcp__docs__search"); got != "docs/search" {
		t.Fatalf("got %s", got)
	}
	if got := DisplayTitle("read"); got != "read" {
		t.Fatalf("got %s", got)
	}
}

func TestProbeReportsResourcesWithoutCounts(t *testing.T) {
	ctx := context.Background()
	statuses, err := Probe(ctx, []Server{
		{
			Name: "demo",
			Config: Config{
				Command: os.Args[0],
				Env:     map[string]string{"PIGO_MCP_FAKE": "stdio"},
				Enabled: true,
			},
		},
		{
			Name:   "off",
			Config: Config{Command: "not-started", Enabled: false},
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	entries := ListEntries(statuses)
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "resourceTemplates") || strings.Contains(text, "resources\":") && strings.Contains(text, "resources\":1") {
		t.Fatalf("counts leaked: %s", text)
	}
	var sawDemo, sawOff bool
	for _, entry := range entries {
		switch entry.Name {
		case "demo":
			sawDemo = true
			if !entry.Resources || entry.Status != "ready" {
				t.Fatalf("demo=%+v", entry)
			}
		case "off":
			sawOff = true
			if entry.Resources || entry.Status != "disabled" {
				t.Fatalf("off=%+v", entry)
			}
		}
	}
	if !sawDemo || !sawOff {
		t.Fatalf("entries=%s", text)
	}
	if strings.Contains(text, `"resources":false`) {
		t.Fatalf("false resources should be omitted: %s", text)
	}
}

func TestLoginTimeoutCoversTheWholeFlow(t *testing.T) {
	hung := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(hung.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := Login(ctx, t.TempDir(), Server{
		Name:   "docs",
		Config: Config{URL: hung.URL + "/mcp", Enabled: true},
	}, oauth.Prompt{Open: func(string) {}})
	if err == nil {
		t.Fatal("login waited for the hung authorization server")
	}
	if err != oauth.ErrSignInCancelled && !strings.Contains(err.Error(), "canceled") && ctx.Err() == nil {
		t.Fatalf("err=%v", err)
	}
}

func TestLogoutRemovesOnlyThatServer(t *testing.T) {
	agent := t.TempDir()
	store := oauth.Open(agent)
	const url = "https://mcp.example/mcp"
	if err := store.Save("docs", url, oauth.Record{Tokens: &oauth.Tokens{AccessToken: "docs"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("other", url, oauth.Record{Tokens: &oauth.Tokens{AccessToken: "other"}}); err != nil {
		t.Fatal(err)
	}
	if err := Logout(agent, Server{Name: "docs", Config: Config{URL: url}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load("docs", url); err != nil || ok {
		t.Fatalf("docs ok=%v err=%v", ok, err)
	}
	if rec, ok, err := store.Load("other", url); err != nil || !ok || rec.Tokens.AccessToken != "other" {
		t.Fatalf("other=%+v ok=%v err=%v", rec.Tokens, ok, err)
	}
}
