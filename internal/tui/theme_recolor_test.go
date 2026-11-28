package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/theme"
)

func TestThemeChangeRecolorsStartupChangelogNotice(t *testing.T) {
	clearTrueColorEnv(t)
	restoreColorProfile(t)
	t.Setenv("PIGO_OFFLINE", "1")

	dir := t.TempDir()
	cfg := testCfg()
	cfg.Theme = "dark"
	cfg.Terminal.TrueColor = "on"
	cfg.LastChangelogVersion = "0.0.0"

	m := New(cfg)
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: cfg}}
	m.applyStartupChangelog()
	if len(m.transcript) == 0 {
		t.Fatal("expected startup changelog notice")
	}

	beforeEntry := m.transcript[0].rendered
	beforeView := m.View()
	if !strings.Contains(stripANSI(beforeEntry), "What's New") {
		t.Fatalf("missing changelog text: %q", beforeEntry)
	}
	if !strings.Contains(beforeEntry, "\x1b[") {
		t.Fatalf("expected ANSI styling in changelog notice: %q", beforeEntry)
	}

	m = submit(m, "/theme light")

	afterEntry := m.transcript[0].rendered
	afterView := m.View()
	if afterEntry == beforeEntry {
		t.Fatalf("changelog notice was not recolored after /theme light:\nbefore=%q\nafter=%q", beforeEntry, afterEntry)
	}
	if !strings.Contains(stripANSI(afterEntry), "What's New") {
		t.Fatalf("changelog text lost after recolor: %q", afterEntry)
	}
	if beforeView == afterView {
		t.Fatal("view did not change after switching theme")
	}

	lightTh := theme.Load("light", "", "")
	darkTh := theme.Load("dark", "", "")
	lightHeading := styleFG(lipgloss.NewStyle(), lightTh.Colors["mdHeading"]).Render("X")
	darkHeading := styleFG(lipgloss.NewStyle(), darkTh.Colors["mdHeading"]).Render("X")
	if lightHeading == darkHeading {
		t.Fatal("expected distinct dark/light mdHeading colors")
	}
	// Extract the SGR foreground color code (e.g. "38;2;143;104;2") without trailing "m"
	// so it matches whether glamour combines Bold (";1m") or not.
	sgrColor := func(rendered string) string {
		prefix, _, ok := strings.Cut(rendered, "X")
		if !ok {
			t.Fatalf("unexpected rendered=%q", rendered)
		}
		prefix = strings.TrimPrefix(prefix, "\x1b[")
		return strings.TrimSuffix(prefix, "m")
	}
	lightCode := sgrColor(lightHeading)
	darkCode := sgrColor(darkHeading)
	if strings.Contains(afterEntry, darkCode) {
		t.Fatalf("stale dark heading ANSI %q still in changelog:\n%q", darkCode, afterEntry)
	}
	if !strings.Contains(afterEntry, lightCode) {
		t.Fatalf("missing light heading ANSI %q in changelog:\n%q", lightCode, afterEntry)
	}
}

func TestThemeChangeRecolorsSlashChangelogAndHistoryMessages(t *testing.T) {
	clearTrueColorEnv(t)
	restoreColorProfile(t)

	agentDir := t.TempDir()
	themesDir := filepath.Join(agentDir, "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customPath := filepath.Join(themesDir, "vivid.json")
	customJSON := `{
  "name": "vivid",
  "appearance": "dark",
  "colors": {
    "accent": "#11aaee",
    "text": "#f0e8d0",
    "userMessageText": "#22cc88",
    "toolTitle": "#ee9933",
    "error": "#ff3355",
    "muted": "#8899aa",
    "mdHeading": "#ff66cc",
    "mdCode": "#44ddff",
    "mdCodeBlock": "#99ee66",
    "mdLink": "#3399ff",
    "mdLinkUrl": "#778899"
  }
}`
	if err := os.WriteFile(customPath, []byte(customJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testCfg()
	cfg.Theme = "dark"
	cfg.Terminal.TrueColor = "on"
	m := New(cfg)
	m.engine = &runtime.Engine{
		Opts:       runtime.Options{AgentDir: agentDir, Config: cfg},
		ThemeFiles: []string{customPath},
	}
	m.loadTheme("dark")

	// 1. /changelog notice
	m = submit(m, "/changelog")
	changelogIdx := len(m.transcript) - 1

	// 2. Plain meta notice & error notice
	m.appendMeta("plain meta notice")
	metaIdx := len(m.transcript) - 1
	m.appendError("fatal error notice")
	errIdx := len(m.transcript) - 1

	// 3. User message, thinking block, assistant markdown message
	m.appendUser("hello pigo")
	userIdx := len(m.transcript) - 1
	m = send(m, agentEventMsg{agent.Event{
		Type: agent.EventMessageEnd,
		Assistant: &ai.AssistantMessage{
			Content: []*ai.Content{
				{Type: ai.KindThinking, Thinking: "deep thought"},
				{Type: ai.KindText, Text: "## Answer\n\nUse `fmt.Println` in Go."},
			},
		},
	}})
	thinkingIdx := userIdx + 1
	assistantIdx := userIdx + 2

	// 4. Bang command entry
	exitCode := 1
	next, _ := m.handleBashDone(bashDoneMsg{
		command: "false",
		result:  runtime.BashResult{Output: "oops", ExitCode: &exitCode},
	})
	m = next.(Model)
	bangIdx := len(m.transcript) - 1

	// 5. Custom tool call entry
	m.appendEntry(entry{role: "tool", kind: entryToolCustom, raw: "custom-tool-line", toolCustom: true})
	customToolIdx := len(m.transcript) - 1

	// 6. Legacy static entry should remain untouched
	m.transcript = append(m.transcript, entry{role: "meta", rendered: "static-legacy-entry"})
	staticIdx := len(m.transcript) - 1

	before := make([]string, len(m.transcript))
	for i, e := range m.transcript {
		before[i] = e.rendered
	}

	// Switch theme via /theme
	m = submit(m, "/theme vivid")

	indices := map[string]int{
		"changelog":  changelogIdx,
		"meta":       metaIdx,
		"error":      errIdx,
		"user":       userIdx,
		"thinking":   thinkingIdx,
		"assistant":  assistantIdx,
		"bang":       bangIdx,
		"customTool": customToolIdx,
	}
	for name, idx := range indices {
		if m.transcript[idx].rendered == before[idx] {
			t.Errorf("%s entry was not recolored:\nbefore=%q\nafter=%q", name, before[idx], m.transcript[idx].rendered)
		}
		if stripANSI(m.transcript[idx].rendered) != stripANSI(before[idx]) {
			t.Errorf("%s plain text changed:\nbefore=%q\nafter=%q", name, stripANSI(before[idx]), stripANSI(m.transcript[idx].rendered))
		}
	}
	if m.transcript[staticIdx].rendered != "static-legacy-entry" {
		t.Fatalf("static entry mutated: %q", m.transcript[staticIdx].rendered)
	}

	// Verify vivid theme colors actually appear in recolored entries.
	wantUser := styleFG(lipgloss.NewStyle().Bold(true), "#22cc88").Render("› you")
	if !strings.HasPrefix(m.transcript[userIdx].rendered, wantUser) {
		t.Errorf("user header did not pick up vivid user color:\ngot=%q\nwant prefix=%q", m.transcript[userIdx].rendered, wantUser)
	}
	wantErr := styleFG(lipgloss.NewStyle(), "#ff3355").Render("fatal error notice")
	if m.transcript[errIdx].rendered != wantErr {
		t.Errorf("error notice mismatch:\ngot=%q\nwant=%q", m.transcript[errIdx].rendered, wantErr)
	}
	wantMeta := styleFG(lipgloss.NewStyle().Faint(true), "#8899aa").Render("plain meta notice")
	if m.transcript[metaIdx].rendered != wantMeta {
		t.Errorf("meta notice mismatch:\ngot=%q\nwant=%q", m.transcript[metaIdx].rendered, wantMeta)
	}
	// Assistant markdown heading should use #ff66cc (RGB 255,102,204).
	if !strings.Contains(m.transcript[assistantIdx].rendered, "38;2;255;102;204") {
		t.Errorf("assistant markdown heading missing vivid mdHeading truecolor:\n%q", m.transcript[assistantIdx].rendered)
	}
}

func TestThemeChangeViaSettingsAndExtRecolorsRestoredHistory(t *testing.T) {
	clearTrueColorEnv(t)
	restoreColorProfile(t)

	cfg := testCfg()
	cfg.Theme = "dark"
	cfg.Terminal.TrueColor = "on"
	m := New(cfg)

	m.history = []ai.Message{
		{Role: ai.RoleUser, Content: "restore me"},
		{Role: ai.RoleAssistant, Content: "# Restored Heading\n\nBody text"},
	}
	m.transcript = transcriptFromMessages(m, m.history)
	beforeUser := m.transcript[0].rendered
	beforeAssistant := m.transcript[1].rendered

	// Switch via settings helper
	m.applySetting("theme", "light")
	if m.transcript[0].rendered == beforeUser {
		t.Fatalf("restored user message not recolored via settings: %q", m.transcript[0].rendered)
	}
	if m.transcript[1].rendered == beforeAssistant {
		t.Fatalf("restored assistant message not recolored via settings: %q", m.transcript[1].rendered)
	}

	lightAssistant := m.transcript[1].rendered
	// Switch via extension setTheme
	res := m.applySetTheme(map[string]any{"name": "dark"})
	if ok, _ := res["success"].(bool); !ok {
		t.Fatalf("applySetTheme failed: %+v", res)
	}
	if m.transcript[1].rendered == lightAssistant {
		t.Fatalf("restored assistant message not recolored via ext setTheme: %q", m.transcript[1].rendered)
	}
	if m.transcript[1].rendered != beforeAssistant {
		t.Fatalf("switching dark->light->dark did not match original:\norig=%q\nnow=%q", beforeAssistant, m.transcript[1].rendered)
	}
}

func TestThemeChangeWithTrueColorOffUsesANSI256(t *testing.T) {
	clearTrueColorEnv(t)
	restoreColorProfile(t)

	cfg := testCfg()
	cfg.Theme = "dark"
	cfg.Terminal.TrueColor = "off"
	m := New(cfg)
	m.appendMarkdownMeta("# Release\n\n- item")
	before := m.transcript[0].rendered
	if strings.Contains(before, "38;2;") {
		t.Fatalf("trueColor=off emitted 24-bit ANSI: %q", before)
	}
	if !strings.Contains(before, "38;5;") {
		t.Fatalf("trueColor=off missing 256-color ANSI: %q", before)
	}

	m = submit(m, "/theme light")
	after := m.transcript[0].rendered
	if after == before {
		t.Fatalf("trueColor=off changelog not recolored:\nbefore=%q\nafter=%q", before, after)
	}
	if strings.Contains(after, "38;2;") {
		t.Fatalf("trueColor=off after theme switch emitted 24-bit ANSI: %q", after)
	}
}
