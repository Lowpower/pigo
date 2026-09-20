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

func TestFooterReplaceHidesDefaultChrome(t *testing.T) {
	m := New(testCfg())
	view := m.View()
	if !strings.Contains(view, "Ctrl+T thinking") {
		t.Fatalf("default footer missing:\n%s", view)
	}
	m.extFooterSet = true
	m.extFooter = []string{"CUSTOM-FOOTER"}
	view = m.View()
	if !strings.Contains(view, "CUSTOM-FOOTER") {
		t.Fatalf("custom footer missing:\n%s", view)
	}
	if strings.Contains(view, "Ctrl+T thinking") {
		t.Fatalf("default chrome still visible:\n%s", view)
	}
}

func TestHeaderAndWorkingMessage(t *testing.T) {
	m := New(testCfg())
	m.extHeader = []string{"CUSTOM-HEADER"}
	on := true
	m.workingVisible = &on
	m.workingMessage = "PLEASE-WAIT"
	view := m.View()
	if !strings.Contains(view, "CUSTOM-HEADER") {
		t.Fatalf("header missing:\n%s", view)
	}
	if !strings.Contains(view, "PLEASE-WAIT") {
		t.Fatalf("working missing:\n%s", view)
	}
}

func TestCustomOverlayRendersWidgets(t *testing.T) {
	m := New(testCfg())
	s := &extCustomState{}
	s.applyOpen(nil, map[string]any{
		"id":      "dlg",
		"overlay": true,
		"widgets": []any{
			map[string]any{"id": "t", "kind": "text", "props": map[string]any{"text": "PICK-ONE"}},
			map[string]any{"id": "s", "kind": "select", "props": map[string]any{"options": []any{"alpha", "beta"}}},
			map[string]any{"id": "skip", "kind": "unknown", "props": map[string]any{"text": "NOPE"}},
		},
	})
	m.extCustom = s
	view := m.View()
	if !strings.Contains(view, "PICK-ONE") || !strings.Contains(view, "alpha") {
		t.Fatalf("overlay missing widgets:\n%s", view)
	}
	if strings.Contains(view, "NOPE") {
		t.Fatalf("unknown kind should be skipped:\n%s", view)
	}
}

func TestGetEditorTextAndPaste(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("typed")
	reply := make(chan map[string]any, 1)
	next, ok := m.handleExtMsg(extUIReqMsg{method: "getEditorText", reply: reply})
	if !ok {
		t.Fatal("expected handle")
	}
	got := <-reply
	if got["text"] != "typed" {
		t.Fatalf("getEditorText=%v", got)
	}
	next, ok = next.handleExtMsg(extPasteMsg{text: " extra"})
	if !ok {
		t.Fatal("paste")
	}
	if !strings.Contains(next.editor.Value(), "typed") || !strings.Contains(next.editor.Value(), "extra") {
		t.Fatalf("after paste %q", next.editor.Value())
	}
}

func TestHiddenThinkingLabel(t *testing.T) {
	m := New(testCfg())
	m.hideThinking = true
	m.thinkingLabel = "hidden-work"
	m.streamingThinking = "secret"
	view := m.View()
	if strings.Contains(view, "secret") {
		t.Fatalf("thinking leaked:\n%s", view)
	}
	if !strings.Contains(view, "hidden-work") {
		t.Fatalf("label missing:\n%s", view)
	}
}
