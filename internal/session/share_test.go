package session

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

func TestWriteShareJSONLAddsPiShareEntry(t *testing.T) {
	dir := t.TempDir()
	m := New(t.TempDir(), dir)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})
	path := dir + "/session.jsonl"
	err := WriteShareJSONL(m, path, "sys", []ai.Tool{{Name: "bash", Description: "run", Parameters: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 4 {
		t.Fatalf("lines=%d %s", len(lines), b)
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatal(err)
	}
	if last["type"] != "custom" || last["customType"] != "pi.share" {
		t.Fatalf("last=%v", last)
	}
	data, _ := last["data"].(map[string]any)
	if data["systemPrompt"] != "sys" {
		t.Fatalf("data=%v", data)
	}
}

func TestShareViewerURLEnvOverride(t *testing.T) {
	t.Setenv("PIGO_SHARE_VIEWER_URL", "https://example.test/s/")
	got := ShareViewerURL("abc123")
	if got != "https://example.test/s/#abc123" {
		t.Fatalf("got %q", got)
	}
}

func TestShareFallsBackToGist(t *testing.T) {
	dir := t.TempDir()
	m := New(t.TempDir(), dir)
	_, _ = m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	_, _ = m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"})

	origRadius, origLook, origAuth, origGist := shareRadius, shareLookPath, shareGHAuthStatus, shareGistCreate
	t.Cleanup(func() {
		shareRadius, shareLookPath, shareGHAuthStatus, shareGistCreate = origRadius, origLook, origAuth, origGist
	})
	shareRadius = func(string, string) (string, bool, error) { return "", false, nil }
	shareLookPath = func(string) (string, error) { return "/bin/gh", nil }
	shareGHAuthStatus = func() error { return nil }
	shareGistCreate = func(string) (string, error) { return "https://gist.github.com/u/deadbeef", nil }

	res, err := Share(ShareOptions{Session: m, ThemeName: "dark", AgentDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.ViewerURL != "https://pi.dev/session/#deadbeef" {
		t.Fatalf("viewer=%q", res.ViewerURL)
	}
	if !strings.Contains(res.String(), "Gist:") {
		t.Fatalf("status=%q", res.String())
	}
}

func TestShareRequiresGH(t *testing.T) {
	dir := t.TempDir()
	m := New(t.TempDir(), dir)
	origRadius, origLook := shareRadius, shareLookPath
	t.Cleanup(func() { shareRadius, shareLookPath = origRadius, origLook })
	shareRadius = func(string, string) (string, bool, error) { return "", false, nil }
	shareLookPath = func(string) (string, error) { return "", errors.New("not found") }
	_, err := Share(ShareOptions{Session: m, AgentDir: dir})
	if err == nil || !strings.Contains(err.Error(), "gh") {
		t.Fatalf("err=%v", err)
	}
}

func TestShareRequiresGHAuth(t *testing.T) {
	dir := t.TempDir()
	m := New(t.TempDir(), dir)
	origRadius, origLook, origAuth := shareRadius, shareLookPath, shareGHAuthStatus
	t.Cleanup(func() { shareRadius, shareLookPath, shareGHAuthStatus = origRadius, origLook, origAuth })
	shareRadius = func(string, string) (string, bool, error) { return "", false, nil }
	shareLookPath = func(string) (string, error) { return "/bin/gh", nil }
	shareGHAuthStatus = func() error { return errors.New(errGHAuth) }
	_, err := Share(ShareOptions{Session: m, AgentDir: dir})
	if err == nil || err.Error() != errGHAuth {
		t.Fatalf("err=%v", err)
	}
}

func TestSharePrefersRadius(t *testing.T) {
	dir := t.TempDir()
	m := New(t.TempDir(), dir)
	orig := shareRadius
	t.Cleanup(func() { shareRadius = orig })
	shareRadius = func(string, string) (string, bool, error) {
		return "https://radius.example/a/1", true, nil
	}
	res, err := Share(ShareOptions{Session: m, AgentDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.RadiusURL != "https://radius.example/a/1" {
		t.Fatalf("%+v", res)
	}
}

func TestTryShareViaRadiusHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/artifacts" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth=%s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artifact":{"canonical_url":"https://radius.example/a"}}`))
	}))
	t.Cleanup(srv.Close)

	origClient, origToken := shareHTTPClient, shareRadiusToken
	t.Cleanup(func() {
		shareHTTPClient = origClient
		shareRadiusToken = origToken
	})
	shareHTTPClient = srv.Client()
	shareRadiusToken = func(string) (string, string, bool) { return "tok", srv.URL, true }

	jsonl := t.TempDir() + "/s.jsonl"
	if err := os.WriteFile(jsonl, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	url, ok, err := tryShareViaRadius(jsonl, t.TempDir())
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if url != "https://radius.example/a" {
		t.Fatalf("url=%q", url)
	}
}
