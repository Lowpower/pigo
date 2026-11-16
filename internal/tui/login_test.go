package tui

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/models"
	pigoruntime "github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/slash"
)

const loginTestURL = "https://example.test/oauth?code=login-403"

func TestLoginOpenAIOffersChatGPT(t *testing.T) {
	next, _ := New(testCfg()).startLogin("openai")
	m := next.(Model)
	if m.login.phase != loginPickType {
		t.Fatalf("phase %v", m.login.phase)
	}
	var labels []string
	for _, opt := range m.login.options {
		labels = append(labels, opt.label)
	}
	joined := strings.Join(labels, "\n")
	if !strings.Contains(joined, "Sign in with ChatGPT") || !strings.Contains(joined, "Sign in with an API key") {
		t.Fatalf("options = %#v", m.login.options)
	}
}

func TestLoginAccountListMarksCodexLegacy(t *testing.T) {
	m := New(testCfg())
	m.login = loginState{
		phase:   loginPickType,
		options: []loginOption{{id: auth.TypeOAuth, label: "Sign in with an account"}},
	}
	next, _ := m.chooseLoginOption()
	got := next.(Model)
	found := map[string]string{}
	for _, opt := range got.login.options {
		if opt.id == "openai" || opt.id == "openai-codex" {
			found[opt.id] = opt.label
		}
	}
	if !strings.HasPrefix(found["openai"], "OpenAI (ChatGPT subscription) [subscription]") {
		t.Fatalf("openai = %q", found["openai"])
	}
	if !strings.HasPrefix(found["openai-codex"], "OpenAI Codex (legacy) [subscription]") {
		t.Fatalf("codex = %q", found["openai-codex"])
	}
}

func TestLoginOAuthLabelsAccountAndSubscription(t *testing.T) {
	dir := t.TempDir()
	if _, err := auth.Open(dir).Modify("anthropic", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: auth.TypeOAuth, Access: "tok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	m := loginModel(t, dir)
	m.login = loginState{
		phase:   loginPickType,
		options: []loginOption{{id: auth.TypeOAuth, label: "Sign in with an account"}},
	}
	next, _ := m.chooseLoginOption()
	got := next.(Model)
	labels := loginLabels(got)
	radius := labels["radius"]
	if !strings.HasPrefix(radius, "Radius [account]") || !strings.Contains(radius, "not configured") || strings.Contains(radius, "subscription") {
		t.Fatalf("radius = %q", radius)
	}
	anth := labels["anthropic"]
	if !strings.HasPrefix(anth, "Anthropic (Claude Pro/Max) [subscription]") || !strings.Contains(anth, "✓ configured") || strings.Contains(anth, "not configured") {
		t.Fatalf("anthropic = %q", anth)
	}
}

func TestLoginAPIKeyLabelKeepsName(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	m := loginModel(t, t.TempDir())
	m.login = loginState{
		phase:   loginPickType,
		options: []loginOption{{id: auth.TypeAPIKey, label: "Sign in with an API key"}},
	}
	next, _ := m.chooseLoginOption()
	got := next.(Model)
	label := loginLabels(got)["openai"]
	if !strings.HasPrefix(label, "OpenAI API key [API key]") || !strings.Contains(label, "✓ env: OPENAI_API_KEY") {
		t.Fatalf("openai = %q", label)
	}
}

func TestLogoutEmptyExplainsScope(t *testing.T) {
	m := loginModel(t, t.TempDir())
	next, _ := m.handleSlash(slash.Command{Name: "logout"})
	got := next.(Model)
	if got.loginActive() {
		t.Fatal("empty logout opened a list")
	}
	text := transcriptText(got)
	if !strings.Contains(text, "No stored credentials to remove") || !strings.Contains(text, "models.json") {
		t.Fatalf("transcript = %s", text)
	}
}

func TestLogoutListsStoredCredentialType(t *testing.T) {
	dir := t.TempDir()
	store := auth.Open(dir)
	if _, err := store.Modify("radius", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: auth.TypeOAuth, Access: "tok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Modify("anthropic", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: auth.TypeOAuth, Access: "tok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	m := loginModel(t, dir)
	next, _ := m.handleSlash(slash.Command{Name: "logout"})
	got := next.(Model)
	if !got.loginActive() || got.login.title != "Select provider to logout:" {
		t.Fatalf("phase %v title %q", got.login.phase, got.login.title)
	}
	labels := loginLabels(got)
	if !strings.Contains(labels["radius"], "[account]") || !strings.Contains(labels["radius"], "✓ configured") {
		t.Fatalf("radius = %q", labels["radius"])
	}
	if !strings.Contains(labels["anthropic"], "[subscription]") || !strings.Contains(labels["anthropic"], "✓ configured") {
		t.Fatalf("anthropic = %q", labels["anthropic"])
	}
	if _, ok := labels["openai"]; ok {
		t.Fatalf("unconfigured provider listed: %q", labels["openai"])
	}
}

func TestLogoutSelectionRemovesStoredOAuth(t *testing.T) {
	dir := t.TempDir()
	if _, err := auth.Open(dir).Modify("anthropic", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: auth.TypeOAuth, Access: "tok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	m := loginModel(t, dir)
	next, _ := m.handleSlash(slash.Command{Name: "logout"})
	got := send(next.(Model), tea.KeyMsg{Type: tea.KeyEnter})
	if got.loginActive() {
		t.Fatal("logout list stayed open")
	}
	if !transcriptContains(got, "Logged out of Anthropic (Claude Pro/Max)") {
		t.Fatalf("transcript = %s", transcriptText(got))
	}
	if _, ok := auth.Get(dir, "anthropic"); ok {
		t.Fatal("credential remains")
	}
}

func TestAuthRowStatus(t *testing.T) {
	if got := formatAuthRow("Radius", auth.TypeOAuth, false, nil); got != "Radius [account] • not configured" {
		t.Fatalf("empty = %q", got)
	}
	if got := authStatusSuffix(auth.TypeAPIKey, true, &auth.Check{Type: auth.TypeOAuth}); got != " • subscription configured" {
		t.Fatalf("subscription mismatch = %q", got)
	}
	if got := authStatusSuffix(auth.TypeAPIKey, false, &auth.Check{Type: auth.TypeOAuth}); got != " • account configured" {
		t.Fatalf("account mismatch = %q", got)
	}
	if got := authStatusSuffix(auth.TypeAPIKey, false, &auth.Check{Type: auth.TypeAPIKey, Source: "key in models.json"}); got != " ✓ key in models.json" {
		t.Fatalf("models.json = %q", got)
	}
	if got := logoutDoneText("openai", auth.TypeAPIKey); got != "Removed stored API key for OpenAI API key. Environment variables and models.json config are unchanged." {
		t.Fatalf("api key logout = %q", got)
	}
}

func TestLogoutNamedProviderStillDeletes(t *testing.T) {
	dir := t.TempDir()
	if err := auth.SetAPIKey(dir, "openai", "sk-test"); err != nil {
		t.Fatal(err)
	}
	m := loginModel(t, dir)
	next, _ := m.handleSlash(slash.Command{Name: "logout", Rest: "openai"})
	got := next.(Model)
	if got.loginActive() {
		t.Fatal("named logout opened a list")
	}
	if !transcriptContains(got, "removed stored key for openai") {
		t.Fatalf("transcript = %s", transcriptText(got))
	}
	if _, ok := auth.Get(dir, "openai"); ok {
		t.Fatal("stored key remains")
	}
}

func loginModel(t *testing.T, dir string) Model {
	t.Helper()
	m := New(testCfg())
	m.engine = &pigoruntime.Engine{Opts: pigoruntime.Options{AgentDir: dir}}
	return m
}

func loginLabels(m Model) map[string]string {
	out := map[string]string{}
	for _, opt := range m.login.options {
		out[opt.id] = opt.label
	}
	return out
}

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
