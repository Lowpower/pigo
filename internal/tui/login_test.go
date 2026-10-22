package tui

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/models"
)

const loginTestURL = "https://example.test/oauth?code=login-403"

func TestLoginDoneWarnsWhenRadiusCatalogMissing(t *testing.T) {
	orig, ok := models.LookupProvider("radius")
	if !ok {
		t.Fatal("radius provider missing")
	}
	t.Cleanup(func() {
		models.RegisterProvider(orig)
		models.ClearOverlays()
	})
	models.ClearOverlays()
	spec := orig
	spec.Models = nil
	models.RegisterProvider(spec)
	m := New(testCfg())
	m.login.provider = "radius"
	m.login.phase = loginRunning
	m = send(m, loginDoneMsg{})
	if m.loginActive() {
		t.Fatal("login UI should close")
	}
	if !transcriptContains(m, "login complete") {
		t.Fatalf("missing login complete: %s", transcriptText(m))
	}
	if !transcriptContains(m, "catalog") {
		t.Fatalf("expected catalog timeout hint, got %s", transcriptText(m))
	}
}

func TestLoginDoneSkipsRadiusHintWhenCatalogReady(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.SetRemoteOverlay("radius", []models.Model{{
		Provider: "radius",
		ID:       "balanced",
		API:      "pigo-messages",
	}})
	m := New(testCfg())
	m.login.provider = "radius"
	m.login.phase = loginRunning
	m = send(m, loginDoneMsg{})
	if transcriptContains(m, "catalog") {
		t.Fatalf("ready catalog should not warn: %s", transcriptText(m))
	}
}

func TestLoginDoneOtherProviderSkipsRadiusHint(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.ClearOverlays()
	m := New(testCfg())
	m.login.provider = "anthropic"
	m.login.phase = loginRunning
	m = send(m, loginDoneMsg{})
	if transcriptContains(m, "catalog") {
		t.Fatalf("non-radius login should not warn: %s", transcriptText(m))
	}
}

func TestLoginAuthURLIsHyperlink(t *testing.T) {
	m := loginShowingAuthURL(t, loginTestURL)
	view := m.View()
	if !strings.Contains(view, "\x1b]8;;"+loginTestURL) {
		t.Fatalf("auth URL was not an OSC 8 hyperlink:\n%s", view)
	}
	if !strings.Contains(view, wantLoginClickHint()) {
		t.Fatalf("missing click hint %q:\n%s", wantLoginClickHint(), view)
	}
	if strings.Contains(stripANSI(view), "Open:") {
		t.Fatalf("auth URL still printed as Open: text:\n%s", stripANSI(view))
	}
	if !strings.Contains(view, "ctrl+x to copy") {
		t.Fatalf("missing copy hint:\n%s", view)
	}
}

func TestLoginCtrlXCopiesAuthURL(t *testing.T) {
	pinHeadlessClipboard(t)
	m := loginShowingAuthURL(t, loginTestURL)
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if got := decodeOSC52(m.clipOSC); got != loginTestURL {
		t.Fatalf("copied %q osc=%q", got, m.clipOSC)
	}
	view := m.View()
	if !strings.Contains(view, "Copied URL to clipboard") {
		t.Fatalf("missing copy confirmation:\n%s", view)
	}
	if !strings.Contains(view, wantLoginClickHint()) {
		t.Fatalf("click hint should stay:\n%s", view)
	}
	if decodeOSC52(view) != loginTestURL {
		t.Fatalf("view should emit the URL via OSC 52:\n%s", view)
	}
}

func TestLoginCtrlXWithoutURLDoesNotCopy(t *testing.T) {
	pinHeadlessClipboard(t)
	m := New(testCfg())
	m.login.phase = loginPickType
	m.login.title = "Select authentication method:"
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.clipOSC != "" {
		t.Fatalf("copied without a URL: %q", m.clipOSC)
	}
}

func TestLoginHyperlinksOffStillCopies(t *testing.T) {
	pinHeadlessClipboard(t)
	m := New(testCfg())
	m.cfg.Terminal.Hyperlinks = false
	m.login.phase = loginRunning
	m.login.title = "Login to anthropic"
	m.login.inbox = make(chan tea.Msg, 1)
	m = send(m, loginEventMsg{ev: auth.Event{Type: auth.EventAuthURL, URL: loginTestURL}})
	view := m.View()
	if strings.Contains(view, "\x1b]8;;") {
		t.Fatalf("hyperlinks off still emitted OSC 8:\n%s", view)
	}
	if !strings.Contains(view, loginTestURL) || !strings.Contains(view, wantLoginClickHint()) {
		t.Fatalf("plain URL or hint missing:\n%s", view)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if got := decodeOSC52(m.clipOSC); got != loginTestURL {
		t.Fatalf("copied %q", got)
	}
}

func TestLoginCopyKeepsURLWhilePrompting(t *testing.T) {
	pinHeadlessClipboard(t)
	m := loginShowingAuthURL(t, loginTestURL)
	reply := make(chan string, 1)
	m = send(m, loginPromptMsg{prompt: auth.Prompt{Message: "paste code"}, reply: reply})
	if m.login.phase != loginPrompting {
		t.Fatalf("phase %d", m.login.phase)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.login.input != "" {
		t.Fatalf("copy key typed into the prompt: %q", m.login.input)
	}
	if got := decodeOSC52(m.clipOSC); got != loginTestURL {
		t.Fatalf("copied %q", got)
	}
}

func TestLoginCopyFailureStaysOnScreen(t *testing.T) {
	pinClipboard(t, "linux", envGet(map[string]string{"DISPLAY": ":0"}))
	stubClipWrite(t, failClipWrite(errors.New("exit status 1")))
	m := loginShowingAuthURL(t, loginTestURL)
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.clipOSC != "" {
		t.Fatalf("clipOSC = %q", m.clipOSC)
	}
	view := m.View()
	if strings.Contains(view, "Copied URL to clipboard") {
		t.Fatalf("success leaked into view:\n%s", view)
	}
	if !strings.Contains(view, "exit status 1") || !strings.Contains(view, wantLoginClickHint()) {
		t.Fatalf("missing failure hint:\n%s", view)
	}
	if transcriptContains(m, "Clipboard unavailable") {
		t.Fatal("failure was written to the transcript")
	}
}

func wantLoginClickHint() string {
	if runtime.GOOS == "darwin" {
		return "Cmd+click to open"
	}
	return "Ctrl+click to open"
}

func loginShowingAuthURL(t *testing.T, url string) Model {
	t.Helper()
	m := New(testCfg())
	m.login.phase = loginRunning
	m.login.title = "Login to anthropic"
	m.login.inbox = make(chan tea.Msg, 1)
	m = send(m, loginEventMsg{ev: auth.Event{Type: auth.EventAuthURL, URL: url}})
	if m.login.phase != loginRunning {
		t.Fatalf("phase %d", m.login.phase)
	}
	return m
}
