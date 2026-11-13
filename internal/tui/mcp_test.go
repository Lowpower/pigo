package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/mcp"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/slash"
)

func TestMCPScreenShowsConnectingServer(t *testing.T) {
	m := New(testCfg())
	m.mcp.active = true
	m.mcp.rows = []mcp.Status{
		{Name: "slow", Status: "connecting", Exposure: "codemode"},
		{Name: "other", Status: "ready", Exposure: "direct", HasResources: true},
	}
	view := m.View()
	if !strings.Contains(view, "slow") || !strings.Contains(view, "connecting") || !strings.Contains(view, "other") {
		t.Fatalf("view=\n%s", view)
	}
	if !strings.Contains(view, "resources") || strings.Contains(view, "resourceTemplates") {
		t.Fatalf("resources line=\n%s", view)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if !strings.Contains(m.View(), "> other") {
		t.Fatalf("cursor:\n%s", m.View())
	}
}

func TestMCPAuthURLCopiesLikeLogin(t *testing.T) {
	pinHeadlessClipboard(t)
	m := New(testCfg())
	m.mcp.active = true
	m.mcp.authURL = loginTestURL
	view := m.View()
	if !strings.Contains(view, "\x1b]8;;"+loginTestURL) {
		t.Fatalf("missing hyperlink:\n%s", view)
	}
	if !strings.Contains(view, wantLoginClickHint()) || !strings.Contains(view, "ctrl+x to copy") {
		t.Fatalf("missing hint:\n%s", view)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if got := decodeOSC52(m.clipOSC); got != loginTestURL {
		t.Fatalf("copied %q", got)
	}
}

func TestMCPDisableWhileAnotherConnects(t *testing.T) {
	stop := make(chan struct{})
	slowSrv := httptest.NewServer(hangMCPServer(stop))
	fastSrv := httptest.NewServer(readyMCPServer())
	t.Cleanup(func() {
		close(stop)
		slowSrv.CloseClientConnections()
		fastSrv.CloseClientConnections()
		slowSrv.Close()
		fastSrv.Close()
	})
	agent := t.TempDir()
	cwd := t.TempDir()
	body := `{"mcpServers":{"slow":{"url":"` + slowSrv.URL + `"},"fast":{"url":"` + fastSrv.URL + `","exposure":"direct"}}}`
	if err := os.WriteFile(filepath.Join(agent, "mcp.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	eng, err := runtime.New(context.Background(), runtime.Options{
		AgentDir: agent, Cwd: cwd, Offline: true, NoExtensions: true, NoSkills: true, NoPromptTpls: true, NoThemes: true,
		Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })

	m := New(testCfg())
	m.engine = eng
	next, _ := m.handleSlash(slash.Command{Name: "mcp"})
	m = next.(Model)
	view := m.View()
	if !strings.Contains(view, "slow") || !strings.Contains(view, "fast") {
		t.Fatalf("screen waited or hid a server:\n%s", view)
	}
	start := time.Now()
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if time.Since(start) > 2*time.Second {
		t.Fatalf("disable waited %s", time.Since(start))
	}
	if !strings.Contains(m.View(), "disabled") {
		t.Fatalf("view=\n%s", m.View())
	}
	var slow mcp.Status
	for _, st := range eng.MCPStatuses() {
		if st.Name == "slow" {
			slow = st
		}
		if st.Name == "fast" && st.Status == "disabled" {
			t.Fatal("fast was disabled with slow")
		}
	}
	if slow.Status != "disabled" {
		t.Fatalf("slow=%+v", slow)
	}
	saved, err := os.ReadFile(filepath.Join(agent, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"enabled": false`) || !strings.Contains(string(saved), fastSrv.URL) {
		t.Fatalf("file=%s", saved)
	}
}

func TestMCPCallTitleAndFiveLineFold(t *testing.T) {
	m := New(testCfg())
	m = send(m, agentEventMsg{agent.Event{
		Type: agent.EventToolStart, ToolName: "mcp__docs__search", ToolCallID: "c1",
	}})
	if !strings.Contains(m.View(), "docs/search") {
		t.Fatalf("title=\n%s", m.View())
	}
	if strings.Contains(m.View(), "mcp__docs__search") {
		t.Fatalf("registered name still shown:\n%s", m.View())
	}
	var body strings.Builder
	for i := 1; i <= 8; i++ {
		body.WriteString("line-")
		body.WriteString(strings.Repeat("x", i))
		body.WriteByte('\n')
	}
	m = send(m, agentEventMsg{agent.Event{
		Type: agent.EventToolEnd, ToolName: "mcp__docs__search", ToolCallID: "c1", Result: body.String(),
	}})
	view := m.View()
	if !strings.Contains(view, "line-"+strings.Repeat("x", 5)) {
		t.Fatalf("missing fifth line:\n%s", view)
	}
	if strings.Contains(view, "line-"+strings.Repeat("x", 6)) {
		t.Fatalf("folded view kept later lines:\n%s", view)
	}
}

func hangMCPServer(stop <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		case <-time.After(30 * time.Second):
		}
	})
}

func readyMCPServer() http.Handler {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fast", Version: "0"}, &sdkmcp.ServerOptions{
		Capabilities: &sdkmcp.ServerCapabilities{Resources: &sdkmcp.ResourceCapabilities{}},
	})
	return sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
}
