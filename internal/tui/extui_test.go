package tui

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/slash"
)

func TestFooterShowsExtensionStatus(t *testing.T) {
	m := New(testCfg())
	m.extStatus = map[string]string{"plan": "PLAN"}
	view := m.View()
	if !strings.Contains(view, "PLAN") {
		t.Fatalf("missing status:\n%s", view)
	}
}

func TestViewRendersExtensionWidget(t *testing.T) {
	m := New(testCfg())
	m.setExtWidget(extWidgetMsg{key: "w", placement: "aboveEditor", lines: []string{"WIDGET-LINE"}})
	view := m.View()
	if !strings.Contains(view, "WIDGET-LINE") {
		t.Fatalf("missing widget:\n%s", view)
	}
}

func TestHelpListsExtensionCommand(t *testing.T) {
	text := slash.HelpTextWith([]slash.Command{{Name: "cmd", Description: "demo slash command"}})
	if !strings.Contains(text, "/cmd") || !strings.Contains(text, "demo slash command") {
		t.Fatalf("help:\n%s", text)
	}
}
