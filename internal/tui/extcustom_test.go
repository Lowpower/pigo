package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func openCustom(m Model, id, text string, overlay bool) Model {
	widgets := []any{
		map[string]any{"id": "t", "kind": "text", "props": map[string]any{"text": text}},
		map[string]any{"id": "s", "kind": "select", "props": map[string]any{"options": []any{"alpha"}}},
	}
	return send(m, extCustomOpMsg{op: "custom.open", args: map[string]any{
		"id": id, "overlay": overlay, "widgets": widgets,
	}})
}

func TestCloseLowerOverlayKeepsUpperResult(t *testing.T) {
	m := openCustom(New(testCfg()), "lower", "LOWER-OVERLAY", true)
	m = openCustom(m, "upper", "UPPER-OVERLAY", true)
	var ev string
	var got map[string]any
	m.extCustomOverlays[1].onEmit = func(name string, args map[string]any) {
		ev = name
		got = args
	}

	m = send(m, extCustomOpMsg{op: "custom.close", args: map[string]any{"id": "lower"}})
	if !m.overlayVisible() {
		t.Fatal("upper overlay should stay visible")
	}
	view := m.View()
	if !strings.Contains(view, "UPPER-OVERLAY") || strings.Contains(view, "LOWER-OVERLAY") {
		t.Fatalf("view after closing lower:\n%s", view)
	}
	if len(m.extCustomOverlays) != 1 || m.extCustomOverlays[0].id != "upper" {
		t.Fatalf("stack = %+v", m.extCustomOverlays)
	}

	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if ev != "custom.submit" || got["id"] != "upper" {
		t.Fatalf("submit event = %s %#v", ev, got)
	}
	if m.visibleOverlay() != nil {
		t.Fatal("submit should close only the upper overlay")
	}
}

func TestCloseWithoutIDLeavesStackedOverlays(t *testing.T) {
	m := openCustom(New(testCfg()), "lower", "LOWER-OVERLAY", true)
	m = openCustom(m, "upper", "UPPER-OVERLAY", true)
	m = send(m, extCustomOpMsg{op: "custom.close"})
	if len(m.extCustomOverlays) != 2 {
		t.Fatalf("close without id popped the stack: %d", len(m.extCustomOverlays))
	}
	m = send(m, extCustomOpMsg{op: "custom.close", args: map[string]any{"id": "missing"}})
	if len(m.extCustomOverlays) != 2 {
		t.Fatalf("unknown id closed an overlay: %d", len(m.extCustomOverlays))
	}
}

func TestCloseOnEmptyStackLeavesDialog(t *testing.T) {
	m := New(testCfg())
	reply := make(chan map[string]any, 1)
	m.extUI = extUIState{active: true, method: "confirm", title: "KEEP-DIALOG", reply: reply}

	m = send(m, extCustomOpMsg{op: "custom.close", args: map[string]any{"id": "gone"}})
	m = send(m, extCustomOpMsg{op: "custom.close"})
	if !m.extUI.active || m.extUI.title != "KEEP-DIALOG" || m.extUI.reply != reply {
		t.Fatalf("dialog changed: %+v", m.extUI)
	}
	select {
	case got := <-reply:
		t.Fatalf("dialog answered: %#v", got)
	default:
	}
}

func TestHideTopOverlayRevealsLower(t *testing.T) {
	m := openCustom(New(testCfg()), "lower", "LOWER-OVERLAY", true)
	m = openCustom(m, "upper", "UPPER-OVERLAY", true)
	m = send(m, extCustomOpMsg{op: "custom.hide", args: map[string]any{"id": "upper"}})
	view := m.View()
	if !strings.Contains(view, "LOWER-OVERLAY") || strings.Contains(view, "UPPER-OVERLAY") {
		t.Fatalf("hidden top should reveal lower:\n%s", view)
	}
}

func TestOverlayCloseLeavesDockCustom(t *testing.T) {
	m := openCustom(New(testCfg()), "dock", "DOCK-CUSTOM", false)
	m = openCustom(m, "over", "UPPER-OVERLAY", true)
	if strings.Contains(m.View(), "DOCK-CUSTOM") {
		t.Fatal("dock custom should stay under the overlay")
	}
	m = send(m, extCustomOpMsg{op: "custom.close", args: map[string]any{"id": "over"}})
	view := m.View()
	if !strings.Contains(view, "DOCK-CUSTOM") {
		t.Fatalf("dock custom missing after overlay close:\n%s", view)
	}
	if m.extCustomDock == nil || m.extCustomDock.id != "dock" {
		t.Fatal("dock slot was cleared with the overlay")
	}
}

func TestUpdateCustomByID(t *testing.T) {
	m := openCustom(New(testCfg()), "lower", "LOWER-OVERLAY", true)
	m = openCustom(m, "upper", "UPPER-OVERLAY", true)
	m = send(m, extCustomOpMsg{op: "custom.update", args: map[string]any{
		"id": "lower",
		"widgets": []any{
			map[string]any{"id": "t", "kind": "text", "props": map[string]any{"text": "LOWER-NEW"}},
		},
	}})
	if got := strArg(m.extCustomOverlays[0].widgets[0].Props, "text"); got != "LOWER-NEW" {
		t.Fatalf("lower text = %q", got)
	}
	if got := strArg(m.extCustomOverlays[1].widgets[0].Props, "text"); got != "UPPER-OVERLAY" {
		t.Fatalf("upper text = %q", got)
	}
	m = send(m, extCustomOpMsg{op: "custom.update", args: map[string]any{
		"widgets": []any{
			map[string]any{"id": "t", "kind": "text", "props": map[string]any{"text": "BOTH"}},
		},
	}})
	if got := strArg(m.extCustomOverlays[1].widgets[0].Props, "text"); got != "UPPER-OVERLAY" {
		t.Fatalf("update without id changed the upper overlay: %q", got)
	}
}
