package tui

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
)

// 1×1 PNG (89 bytes).
const png1x1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectImageProtocol(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "unknown", env: map[string]string{}, want: ""},
		{name: "tmux blocks kitty", env: map[string]string{"TMUX": "/tmp/tmux", "KITTY_WINDOW_ID": "1"}, want: ""},
		{name: "term tmux", env: map[string]string{"TERM": "tmux-256color", "TERM_PROGRAM": "iterm.app"}, want: ""},
		{name: "screen", env: map[string]string{"TERM": "screen-256color", "KITTY_WINDOW_ID": "1"}, want: ""},
		{name: "kitty env", env: map[string]string{"KITTY_WINDOW_ID": "1"}, want: protoKitty},
		{name: "kitty program", env: map[string]string{"TERM_PROGRAM": "kitty"}, want: protoKitty},
		{name: "ghostty", env: map[string]string{"TERM_PROGRAM": "ghostty"}, want: protoKitty},
		{name: "ghostty term", env: map[string]string{"TERM": "xterm-ghostty"}, want: protoKitty},
		{name: "wezterm", env: map[string]string{"WEZTERM_PANE": "0"}, want: protoKitty},
		{name: "warp program", env: map[string]string{"TERM_PROGRAM": "WarpTerminal"}, want: protoKitty},
		{name: "warp session", env: map[string]string{"WARP_SESSION_ID": "abc"}, want: protoKitty},
		{name: "iterm session", env: map[string]string{"ITERM_SESSION_ID": "w0t0p0:x"}, want: protoITerm},
		{name: "iterm program", env: map[string]string{"TERM_PROGRAM": "iTerm.app"}, want: protoITerm},
		{name: "vscode", env: map[string]string{"TERM_PROGRAM": "vscode"}, want: ""},
		{name: "windows terminal", env: map[string]string{"WT_SESSION": "s", "TERM": "xterm-256color"}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectImageProtocol(envOf(tc.env)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func pngHeader(w, h int) string {
	b := make([]byte, 24)
	copy(b, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	binary.BigEndian.PutUint32(b[16:20], uint32(w))
	binary.BigEndian.PutUint32(b[20:24], uint32(h))
	return base64.StdEncoding.EncodeToString(b)
}

func TestKittyPlacementAspect(t *testing.T) {
	cases := []struct {
		name          string
		w, h, maxCols int
		wantCols      int
		wantRows      int
	}{
		{name: "wide", w: 615, h: 86, maxCols: 60, wantCols: 60, wantRows: 4},
		{name: "tall", w: 400, h: 900, maxCols: 60, wantCols: 60, wantRows: 68},
		{name: "wide capped", w: 615, h: 86, maxCols: 30, wantCols: 30, wantRows: 2},
		{name: "square pixel", w: 1, h: 1, maxCols: 60, wantCols: 60, wantRows: 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cols, rows := kittyPlacement(tc.w, tc.h, tc.maxCols)
			if cols != tc.wantCols || rows != tc.wantRows {
				t.Fatalf("got %dx%d want %dx%d", cols, rows, tc.wantCols, tc.wantRows)
			}
		})
	}
}

func TestEncodeKittyWritesRows(t *testing.T) {
	got := encodeKitty("AAAA", 60, 4)
	if !strings.Contains(got, "a=T,f=100,q=2,c=60,r=4;") {
		t.Fatalf("got %q", got)
	}
}

func TestEncodeKittyOmitsUnknownRows(t *testing.T) {
	got := encodeKitty("AAAA", 60, 0)
	if strings.Contains(got, "r=") {
		t.Fatalf("rows should be omitted: %q", got)
	}
	if !strings.Contains(got, "a=T,f=100,q=2,c=60;") {
		t.Fatalf("got %q", got)
	}
}

func TestEncodeKittyChunks(t *testing.T) {
	data := strings.Repeat("A", 5000)
	got := encodeKitty(data, 60, 0)
	if !strings.Contains(got, ",m=1;") {
		t.Fatalf("missing first chunk: %q", got[:80])
	}
	if !strings.Contains(got, "\x1b_Gm=0;") {
		t.Fatalf("missing last chunk: %q", got[len(got)-40:])
	}
	if strings.Count(got, "\x1b_G") < 2 {
		t.Fatalf("want at least 2 frames, got %d", strings.Count(got, "\x1b_G"))
	}
}

func TestImageFallbackWithPNGDims(t *testing.T) {
	img := ai.ImageContent{Type: "image", Data: png1x1, MimeType: "image/png"}
	got := imageFallback(img)
	if got != "[Image: [image/png] 1x1]" {
		t.Fatalf("got %q", got)
	}
}

func TestSplitToolImagesFromContentJSON(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"screenshot ok"},{"type":"image","data":"` + png1x1 + `","mimeType":"image/png"}]}`
	text, imgs := splitToolImages(raw)
	if text != "screenshot ok" {
		t.Fatalf("text=%q", text)
	}
	if len(imgs) != 1 || imgs[0].MimeType != "image/png" || imgs[0].Data != png1x1 {
		t.Fatalf("imgs=%+v", imgs)
	}
}

func TestSplitToolImagesFromDataURL(t *testing.T) {
	raw := "see data:image/png;base64," + png1x1 + " done"
	text, imgs := splitToolImages(raw)
	if !strings.Contains(text, "see") || !strings.Contains(text, "done") {
		t.Fatalf("text=%q", text)
	}
	if strings.Contains(text, "data:image") {
		t.Fatalf("data URL should be stripped: %q", text)
	}
	if len(imgs) != 1 || imgs[0].MimeType != "image/png" {
		t.Fatalf("imgs=%+v", imgs)
	}
}

func TestSplitToolImagesPlainText(t *testing.T) {
	text, imgs := splitToolImages("# pigo\nmore")
	if text != "# pigo\nmore" {
		t.Fatalf("text=%q", text)
	}
	if len(imgs) != 0 {
		t.Fatalf("imgs=%d", len(imgs))
	}
}

func TestViewInlinesKittyImage(t *testing.T) {
	m := New(testCfg())
	m.imgProto = protoKitty
	raw := `{"content":[{"type":"text","text":"shot"},{"type":"image","data":"` + png1x1 + `","mimeType":"image/png"}]}`
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: raw}})
	view := m.View()
	if !strings.Contains(view, "shot") {
		t.Fatalf("missing tool text:\n%s", view)
	}
	if strings.Contains(view, `"content"`) {
		t.Fatalf("raw JSON leaked:\n%s", view)
	}
	if !strings.Contains(view, "\x1b_Ga=T,f=100,q=2,c=60,r=30;") {
		t.Fatalf("missing kitty sequence:\n%s", view)
	}
	if !strings.Contains(view, png1x1) {
		t.Fatalf("missing payload:\n%s", view)
	}
}

func TestViewFallsBackWhenShowImagesOff(t *testing.T) {
	m := New(testCfg())
	m.imgProto = protoKitty
	off := false
	m.cfg.Terminal.ShowImages = &off
	raw := `{"content":[{"type":"image","data":"` + png1x1 + `","mimeType":"image/png"}]}`
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: raw}})
	view := m.View()
	if strings.Contains(view, "\x1b_G") {
		t.Fatalf("should not inline when disabled:\n%s", view)
	}
	if !strings.Contains(view, "[Image: [image/png] 1x1]") {
		t.Fatalf("missing fallback:\n%s", view)
	}
}

func TestViewFallsBackWithoutProtocol(t *testing.T) {
	m := New(testCfg())
	m.imgProto = ""
	raw := `{"content":[{"type":"image","data":"` + png1x1 + `","mimeType":"image/png"}]}`
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: raw}})
	view := m.View()
	if strings.Contains(view, "\x1b_G") || strings.Contains(view, "\x1b]1337;") {
		t.Fatalf("should not inline:\n%s", view)
	}
	if !strings.Contains(view, "[Image: [image/png] 1x1]") {
		t.Fatalf("missing fallback:\n%s", view)
	}
}

func TestViewKittySizesWideAndTall(t *testing.T) {
	cases := []struct {
		name  string
		w, h  int
		width int
		want  string
	}{
		{name: "wide", w: 615, h: 86, want: "c=60,r=4;"},
		{name: "tall", w: 400, h: 900, want: "c=60,r=68;"},
		{name: "wide capped", w: 615, h: 86, width: 30, want: "c=30,r=2;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testCfg()
			if tc.width > 0 {
				w := tc.width
				cfg.Terminal.ImageWidthCells = &w
			}
			m := New(cfg)
			m.imgProto = protoKitty
			data := pngHeader(tc.w, tc.h)
			raw := `{"content":[{"type":"image","data":"` + data + `","mimeType":"image/png"}]}`
			m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: raw}})
			view := m.View()
			if !strings.Contains(view, "\x1b_Ga=T,f=100,q=2,"+tc.want) {
				t.Fatalf("missing %s:\n%s", tc.want, view)
			}
		})
	}
}

func TestViewKittyOmitsRowsWhenPNGSizeUnknown(t *testing.T) {
	m := New(testCfg())
	m.imgProto = protoKitty
	raw := `{"content":[{"type":"image","data":"AAAA","mimeType":"image/png"}]}`
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: raw}})
	view := m.View()
	if strings.Contains(view, "r=") {
		t.Fatalf("rows should be omitted:\n%s", view)
	}
	if !strings.Contains(view, "\x1b_Ga=T,f=100,q=2,c=60;") {
		t.Fatalf("missing kitty sequence:\n%s", view)
	}
}

func TestViewInlinesITermImage(t *testing.T) {
	m := New(testCfg())
	m.imgProto = protoITerm
	raw := `{"content":[{"type":"image","data":"` + png1x1 + `","mimeType":"image/png"}]}`
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: raw}})
	view := m.View()
	decoded, err := base64.StdEncoding.DecodeString(png1x1)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("\x1b]1337;File=inline=1;width=60;size=%d:%s\x07", len(decoded), png1x1)
	if !strings.Contains(view, want) {
		t.Fatalf("missing iterm sequence:\n%s", view)
	}
}

func TestKittyFallsBackForJPEG(t *testing.T) {
	m := New(config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Theme: "default"})
	m.imgProto = protoKitty
	raw := `{"content":[{"type":"image","data":"AAAA","mimeType":"image/jpeg"}]}`
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventToolEnd, ToolName: "read", Result: raw}})
	view := m.View()
	if strings.Contains(view, "\x1b_G") {
		t.Fatalf("kitty cannot inline jpeg:\n%s", view)
	}
	if !strings.Contains(view, "[Image: [image/jpeg]]") {
		t.Fatalf("missing jpeg fallback:\n%s", view)
	}
}
