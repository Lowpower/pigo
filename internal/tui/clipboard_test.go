package tui

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/ai"
)

func stubRunClip(t *testing.T, fn func(string, time.Duration, ...string) ([]byte, bool)) {
	t.Helper()
	old := runClip
	runClip = fn
	t.Cleanup(func() { runClip = old })
}

func clipArgs(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func TestReadXclipImageSkipsUnadvertisedImageMIME(t *testing.T) {
	var cmds []string
	stubRunClip(t, func(name string, _ time.Duration, args ...string) ([]byte, bool) {
		cmds = append(cmds, clipArgs(name, args))
		if name == "xclip" && strings.Contains(clipArgs(name, args), "TARGETS") {
			return []byte("TARGETS\nUTF8_STRING\nSTRING\n"), true
		}
		return []byte("hello"), true
	})
	if img := readXclipImage(); img != nil {
		t.Fatalf("image = %#v", img)
	}
	if len(cmds) != 1 {
		t.Fatalf("cmds = %#v", cmds)
	}
}

func TestReadXclipImageSkipsWhenTargetsFail(t *testing.T) {
	var cmds []string
	stubRunClip(t, func(name string, _ time.Duration, args ...string) ([]byte, bool) {
		cmds = append(cmds, clipArgs(name, args))
		return nil, false
	})
	if img := readXclipImage(); img != nil {
		t.Fatalf("image = %#v", img)
	}
	if len(cmds) != 1 {
		t.Fatalf("cmds = %#v", cmds)
	}
}

func TestReadXclipImageReadsAdvertisedPNG(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	var imageReqs int
	stubRunClip(t, func(name string, _ time.Duration, args ...string) ([]byte, bool) {
		if name != "xclip" {
			t.Fatalf("command = %s", name)
		}
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "TARGETS") {
			return []byte("TARGETS\nUTF8_STRING\nimage/png\n"), true
		}
		imageReqs++
		if !strings.Contains(joined, "image/png") {
			t.Fatalf("args = %v", args)
		}
		return png, true
	})
	img := readXclipImage()
	if img == nil || img.mime != "image/png" || len(img.bytes) == 0 {
		t.Fatalf("image = %#v", img)
	}
	if imageReqs != 1 {
		t.Fatalf("image requests = %d", imageReqs)
	}
}

func TestReadWlPasteImageSkipsUnlistedImageType(t *testing.T) {
	var cmds []string
	stubRunClip(t, func(name string, _ time.Duration, args ...string) ([]byte, bool) {
		cmds = append(cmds, clipArgs(name, args))
		if name == "wl-paste" && strings.Contains(strings.Join(args, " "), "--list-types") {
			return []byte("text/plain\ntext/plain;charset=utf-8\n"), true
		}
		return []byte("hello"), true
	})
	if img := readWlPasteImage(); img != nil {
		t.Fatalf("image = %#v", img)
	}
	if len(cmds) != 1 || !strings.Contains(cmds[0], "--list-types") || strings.Contains(cmds[0], "--type") {
		t.Fatalf("cmds = %#v", cmds)
	}
}

func TestWSLCopyWritesPowerShellFromFile(t *testing.T) {
	var cmds []string
	old := runClipCmd
	runClipCmd = func(name string, _ time.Duration, args ...string) ([]byte, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		if name == "wslpath" {
			return []byte("C:\\tmp\\clip.txt\r\n"), nil
		}
		return []byte("ok"), nil
	}
	t.Cleanup(func() { runClipCmd = old })

	getenv := func(k string) string {
		if k == "WSL_DISTRO_NAME" {
			return "Ubuntu"
		}
		return ""
	}
	if !copyWindowsClipboard("你好", getenv, "linux") {
		t.Fatal("expected windows clipboard write")
	}
	if len(cmds) != 2 || !strings.HasPrefix(cmds[0], "wslpath -w ") {
		t.Fatalf("cmds = %#v", cmds)
	}
	if !strings.Contains(cmds[1], "powershell.exe") || !strings.Contains(cmds[1], "Set-Clipboard") || !strings.Contains(cmds[1], "UTF8") {
		t.Fatalf("powershell = %s", cmds[1])
	}
	if copyWindowsClipboard("x", func(string) string { return "" }, "linux") {
		t.Fatal("non-WSL linux should skip windows clipboard")
	}
	if copyWindowsClipboard("x", getenv, "darwin") {
		t.Fatal("non-linux should skip windows clipboard")
	}
}

func TestParseClipboardFilePaths(t *testing.T) {
	paths := parseClipboardFilePaths([]byte(" [\"/tmp/a.png\", \"/tmp/My Photos/b.png\"]\n"))
	if strings.Join(paths, "|") != "/tmp/a.png|/tmp/My Photos/b.png" {
		t.Fatalf("paths = %#v", paths)
	}
	if parseClipboardFilePaths([]byte("[]")) != nil {
		t.Fatal("empty array should be no paths")
	}
	if parseClipboardFilePaths([]byte("not-json")) != nil {
		t.Fatal("invalid json should be no paths")
	}
	if parseClipboardFilePaths([]byte(`[""]`)) != nil {
		t.Fatal("blank paths should be dropped")
	}
	kept := parseClipboardFilePaths([]byte(`["/tmp/ok.png",""]`))
	if strings.Join(kept, "|") != "/tmp/ok.png" {
		t.Fatalf("kept = %#v", kept)
	}
	raw := parseClipboardFilePaths([]byte(`["/tmp/photo\u001b]0;unsafe\u0007.png"]`))
	if len(raw) != 1 || !strings.Contains(raw[0], "\x1b") || !strings.Contains(raw[0], "\x07") {
		t.Fatalf("control path = %#v", raw)
	}
}

func TestReadDarwinClipboardFilePaths(t *testing.T) {
	var cmds []string
	stubRunClip(t, func(name string, _ time.Duration, args ...string) ([]byte, bool) {
		cmds = append(cmds, clipArgs(name, args))
		if name != "osascript" {
			return nil, false
		}
		return []byte(`["/tmp/screenshot.png","/tmp/My Photos/photo.png"]`), true
	})
	paths := readDarwinClipboardFilePaths()
	if strings.Join(paths, "|") != "/tmp/screenshot.png|/tmp/My Photos/photo.png" {
		t.Fatalf("paths = %#v", paths)
	}
	if len(cmds) != 1 || !strings.Contains(cmds[0], "-l JavaScript") || !strings.Contains(cmds[0], "NSPasteboardURLReadingFileURLsOnlyKey") || !strings.Contains(cmds[0], "JSON.stringify") || !strings.Contains(cmds[0], "isNil") {
		t.Fatalf("cmd = %#v", cmds)
	}
}

func TestReadDarwinClipboardFilePathsMisses(t *testing.T) {
	stubRunClip(t, func(string, time.Duration, ...string) ([]byte, bool) {
		return []byte("[]\n"), true
	})
	if readDarwinClipboardFilePaths() != nil {
		t.Fatal("empty file URL list should miss")
	}
	stubRunClip(t, func(string, time.Duration, ...string) ([]byte, bool) {
		return nil, false
	})
	if readDarwinClipboardFilePaths() != nil {
		t.Fatal("osascript failure should miss")
	}
}

func TestReadClipboardFilePathsSkipsNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("guard is for other platforms")
	}
	called := false
	stubRunClip(t, func(string, time.Duration, ...string) ([]byte, bool) {
		called = true
		return []byte(`["/tmp/a"]`), true
	})
	if readClipboardFilePaths() != nil || called {
		t.Fatalf("called=%v", called)
	}
}

func pinHeadlessClipboard(t *testing.T) {
	t.Helper()
	pinClipboard(t, "linux", func(string) string { return "" })
}

func pinClipboard(t *testing.T, goos string, getenv func(string) string) {
	t.Helper()
	oldOS, oldEnv := clipboardGOOS, clipboardGetenv
	clipboardGOOS = goos
	clipboardGetenv = getenv
	t.Cleanup(func() {
		clipboardGOOS = oldOS
		clipboardGetenv = oldEnv
	})
}

func TestCopyFailureFlashNotSuccess(t *testing.T) {
	if copyErrorFlashDuration != 5*time.Second {
		t.Fatalf("failure flash = %s", copyErrorFlashDuration)
	}
	pinClipboard(t, "linux", envGet(map[string]string{"DISPLAY": ":0"}))
	stubClipWrite(t, failClipWrite(errors.New("exit status 1")))
	m := New(testCfg())
	m.history = []ai.Message{{Role: ai.RoleAssistant, Content: "copy-me"}}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.clipOSC != "" {
		t.Fatalf("clipOSC = %q", m.clipOSC)
	}
	view := m.View()
	if strings.Contains(view, "copied last assistant message") {
		t.Fatalf("success leaked into view:\n%s", view)
	}
	if !strings.Contains(view, "install xclip or xsel, or check X11 access") || !strings.Contains(view, "exit status 1") {
		t.Fatalf("missing failure flash:\n%s", view)
	}
	for _, e := range m.transcript {
		if strings.Contains(e.rendered, "Clipboard unavailable") {
			t.Fatalf("failure was written to the transcript: %q", e.rendered)
		}
	}
	gen := m.copyFlashGen
	m = send(m, copyFlashClearMsg{gen: gen - 1})
	if !strings.Contains(m.View(), "exit status 1") {
		t.Fatal("stale clear should keep the failure")
	}
	m = send(m, copyFlashClearMsg{gen: gen})
	if strings.Contains(m.View(), "exit status 1") || m.copyFlash != "" {
		t.Fatal("failure flash should clear")
	}

	m = submit(m, "/copy")
	view = m.View()
	if strings.Contains(view, "copied last assistant message") || !strings.Contains(view, "exit status 1") {
		t.Fatalf("slash copy view:\n%s", view)
	}
}

func TestTreeCopyFailureClearsStatus(t *testing.T) {
	pinClipboard(t, "linux", envGet(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}))
	stubClipWrite(t, failClipWrite(errors.New("exit status 1")))
	m := treeModel(t)
	m = submit(m, "/tree")
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if strings.Contains(m.tree.status, "Copied selected") {
		t.Fatalf("status = %q", m.tree.status)
	}
	if !strings.Contains(m.tree.status, "install wl-clipboard (wl-copy)") || !strings.Contains(m.View(), "exit status 1") {
		t.Fatalf("status = %q\n%s", m.tree.status, m.View())
	}
	kept := m.tree.status
	m.tree.status = "Already at this point"
	m = send(m, copyFlashClearMsg{gen: m.copyFlashGen})
	if m.tree.status != "Already at this point" {
		t.Fatalf("clear should leave a newer status, got %q (was %q)", m.tree.status, kept)
	}
	m.tree.status = kept
	m.copyFlash = kept
	m.copyFlashTree = true
	m = send(m, copyFlashClearMsg{gen: m.copyFlashGen})
	if m.tree.status != "" {
		t.Fatalf("status = %q", m.tree.status)
	}
}

func envGet(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func stubClipWrite(t *testing.T, fn func(string, time.Duration, string, ...string) error) *[]string {
	t.Helper()
	var cmds []string
	old := runClipWrite
	runClipWrite = func(name string, d time.Duration, input string, args ...string) error {
		if d != clipWriteTimeout {
			t.Errorf("timeout = %s", d)
		}
		line := name
		if len(args) > 0 {
			line += " " + strings.Join(args, " ")
		}
		cmds = append(cmds, line+"\n"+input)
		return fn(name, d, input, args...)
	}
	t.Cleanup(func() { runClipWrite = old })
	return &cmds
}

func okClipWrite(string, time.Duration, string, ...string) error { return nil }

func failClipWrite(err error) func(string, time.Duration, string, ...string) error {
	return func(string, time.Duration, string, ...string) error { return err }
}

func TestCopyWaylandStopsAtWlCopy(t *testing.T) {
	cmds := stubClipWrite(t, okClipWrite)
	osc, err := copyToClipboard("hello", "linux", envGet(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}))
	if err != nil || osc != "" {
		t.Fatalf("osc=%q err=%v", osc, err)
	}
	if len(*cmds) != 1 || (*cmds)[0] != "wl-copy\nhello" {
		t.Fatalf("cmds = %#v", *cmds)
	}
}

func TestCopyWaylandFallsThroughToX11(t *testing.T) {
	cmds := stubClipWrite(t, func(name string, _ time.Duration, input string, _ ...string) error {
		if input != "你好" {
			t.Fatalf("input = %q", input)
		}
		if name == "xsel" {
			return nil
		}
		return errors.New("exit status 1")
	})
	env := envGet(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"})
	osc, err := copyToClipboard("你好", "linux", env)
	if err != nil || osc != "" {
		t.Fatalf("osc=%q err=%v", osc, err)
	}
	want := []string{"wl-copy", "xclip -selection clipboard", "xsel --clipboard --input"}
	if len(*cmds) != len(want) {
		t.Fatalf("cmds = %#v", *cmds)
	}
	for i, prefix := range want {
		if !strings.HasPrefix((*cmds)[i], prefix+"\n") {
			t.Fatalf("cmd %d = %q", i, (*cmds)[i])
		}
	}
}

func TestCopyX11StopsAtXclip(t *testing.T) {
	cmds := stubClipWrite(t, okClipWrite)
	osc, err := copyToClipboard("x", "linux", envGet(map[string]string{"DISPLAY": ":0"}))
	if err != nil || osc != "" {
		t.Fatalf("osc=%q err=%v", osc, err)
	}
	if len(*cmds) != 1 || !strings.HasPrefix((*cmds)[0], "xclip -selection clipboard\n") {
		t.Fatalf("cmds = %#v", *cmds)
	}
}

func TestCopyDarwinAndWindowsCommands(t *testing.T) {
	cmds := stubClipWrite(t, okClipWrite)
	if _, err := copyToClipboard("mac", "darwin", envGet(nil)); err != nil || len(*cmds) != 1 || !strings.HasPrefix((*cmds)[0], "pbcopy\nmac") {
		t.Fatalf("darwin cmds = %#v", *cmds)
	}
	*cmds = nil
	if _, err := copyToClipboard("win", "windows", envGet(nil)); err != nil || len(*cmds) != 1 || !strings.HasPrefix((*cmds)[0], "clip\nwin") {
		t.Fatalf("windows cmds = %#v", *cmds)
	}
}

func TestCopyTermuxCommand(t *testing.T) {
	cmds := stubClipWrite(t, okClipWrite)
	_, err := copyToClipboard("t", "linux", envGet(map[string]string{"TERMUX_VERSION": "0.118"}))
	if err != nil || len(*cmds) != 1 || !strings.HasPrefix((*cmds)[0], "termux-clipboard-set\n") {
		t.Fatalf("cmds = %#v err=%v", *cmds, err)
	}
}

func TestCopyDesktopFailureReportsHintAndCommandError(t *testing.T) {
	stubClipWrite(t, failClipWrite(errors.New("exit status 1")))
	osc, err := copyToClipboard("x", "linux", envGet(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}))
	if osc != "" || err == nil {
		t.Fatalf("osc=%q err=%v", osc, err)
	}
	got := err.Error()
	if !strings.Contains(got, "install wl-clipboard (wl-copy) or check Wayland access") || !strings.Contains(got, "exit status 1") {
		t.Fatalf("err = %q", got)
	}
	osc, err = copyToClipboard("x", "linux", envGet(map[string]string{"DISPLAY": ":0"}))
	if osc != "" || err == nil || !strings.Contains(err.Error(), "install xclip or xsel, or check X11 access") || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("osc=%q err=%v", osc, err)
	}
	stubClipWrite(t, failClipWrite(errors.New("executable file not found")))
	_, err = copyToClipboard("x", "linux", envGet(map[string]string{"TERMUX_VERSION": "1", "WAYLAND_DISPLAY": "wayland-0"}))
	if err == nil || !strings.Contains(err.Error(), "install the Termux:API app and termux-api package") || !strings.Contains(err.Error(), "executable file not found") {
		t.Fatalf("termux err = %v", err)
	}
	_, err = copyToClipboard("x", "darwin", envGet(nil))
	if err == nil || !strings.HasPrefix(err.Error(), "Clipboard unavailable:") || strings.Contains(err.Error(), "wl-clipboard") {
		t.Fatalf("darwin err = %v", err)
	}
}

func TestCopyHeadlessEmitsOSC52(t *testing.T) {
	cmds := stubClipWrite(t, failClipWrite(errors.New("should not run")))
	osc, err := copyToClipboard("hello", "linux", envGet(nil))
	if err != nil || osc != osc52("hello") {
		t.Fatalf("osc=%q err=%v", osc, err)
	}
	if len(*cmds) != 0 {
		t.Fatalf("cmds = %#v", *cmds)
	}
	osc, err = copyToClipboard(strings.Repeat("a", 75000), "linux", envGet(nil))
	if err != nil || osc == "" {
		t.Fatalf("boundary osc len=%d err=%v", len(osc), err)
	}
}

func TestCopyOversizedHeadlessFails(t *testing.T) {
	_, err := copyToClipboard(strings.Repeat("a", 75001), "linux", envGet(nil))
	if err == nil || err.Error() != "Clipboard unavailable: text exceeds the OSC 52 size limit" {
		t.Fatalf("err = %v", err)
	}
}

func TestCopyRemoteAppendsOSC52AfterNativeSuccess(t *testing.T) {
	stubClipWrite(t, okClipWrite)
	osc, err := copyToClipboard("remote", "darwin", envGet(map[string]string{"SSH_CONNECTION": "1"}))
	if err != nil || osc != osc52("remote") {
		t.Fatalf("ssh osc=%q err=%v", osc, err)
	}
	osc, err = copyToClipboard("remote", "darwin", envGet(map[string]string{"PI_CLIPBOARD_OSC52": "1"}))
	if err != nil || osc != osc52("remote") {
		t.Fatalf("flag osc=%q err=%v", osc, err)
	}
	osc, err = copyToClipboard("remote", "darwin", envGet(map[string]string{"SSH_CLIENT": "1"}))
	if err != nil || osc != osc52("remote") {
		t.Fatalf("client osc=%q err=%v", osc, err)
	}
	osc, err = copyToClipboard("remote", "linux", envGet(map[string]string{
		"MOSH_CONNECTION": "1",
		"DISPLAY":         ":0",
	}))
	if err != nil || osc != osc52("remote") {
		t.Fatalf("mosh osc=%q err=%v", osc, err)
	}
}

func TestCopyRemoteDesktopFailureStillEmitsOSC52(t *testing.T) {
	stubClipWrite(t, failClipWrite(errors.New("exit status 1")))
	osc, err := copyToClipboard("via-ssh", "linux", envGet(map[string]string{
		"DISPLAY":        ":0",
		"SSH_CONNECTION": "1",
	}))
	if err != nil || osc != osc52("via-ssh") {
		t.Fatalf("osc=%q err=%v", osc, err)
	}
}

func TestCopyWSLPrefersWindowsTerminalOSC52(t *testing.T) {
	stubClipWrite(t, failClipWrite(errors.New("should not run")))
	called := false
	old := runClipCmd
	runClipCmd = func(string, time.Duration, ...string) ([]byte, error) {
		called = true
		return nil, errors.New("powershell")
	}
	t.Cleanup(func() { runClipCmd = old })
	env := envGet(map[string]string{"WSL_DISTRO_NAME": "Ubuntu", "WT_SESSION": "1"})
	osc, err := copyToClipboard("wsl", "linux", env)
	if err != nil || osc != osc52("wsl") || called {
		t.Fatalf("osc=%q err=%v called=%v", osc, err, called)
	}
}

func TestCopyWSLUsesPowerShellThenHeadlessOSC52(t *testing.T) {
	stubClipWrite(t, failClipWrite(errors.New("should not run")))
	old := runClipCmd
	runClipCmd = func(name string, _ time.Duration, _ ...string) ([]byte, error) {
		if name == "wslpath" {
			return []byte("C:\\tmp\\clip.txt\r\n"), nil
		}
		return []byte("ok"), nil
	}
	t.Cleanup(func() { runClipCmd = old })
	env := envGet(map[string]string{"WSL_DISTRO_NAME": "Ubuntu"})
	osc, err := copyToClipboard("你好", "linux", env)
	if err != nil || osc != "" {
		t.Fatalf("powershell osc=%q err=%v", osc, err)
	}

	runClipCmd = func(string, time.Duration, ...string) ([]byte, error) {
		return nil, errors.New("powershell failed")
	}
	osc, err = copyToClipboard("你好", "linux", env)
	if err != nil || osc != osc52("你好") {
		t.Fatalf("fallback osc=%q err=%v", osc, err)
	}

	osc, err = copyToClipboard("x", "linux", envGet(map[string]string{
		"WSL_DISTRO_NAME": "Ubuntu",
		"DISPLAY":         ":0",
	}))
	if osc != "" || err == nil || !strings.Contains(err.Error(), "install xclip or xsel") || !strings.Contains(err.Error(), "powershell failed") {
		t.Fatalf("display osc=%q err=%v", osc, err)
	}
}

func TestFormatClipboardFilePaths(t *testing.T) {
	got, ok := formatClipboardFilePaths([]string{"/tmp/a.png", "/tmp/My Photos/b.png"}, false)
	if !ok || got != "/tmp/a.png\n/tmp/My Photos/b.png" {
		t.Fatalf("plain = %q ok=%v", got, ok)
	}
	got, ok = formatClipboardFilePaths([]string{
		"/tmp/My Photos/photo.png",
		"/tmp/$(touch hacked).png",
		"/tmp/plain.png",
		"/tmp/it's.png",
	}, true)
	want := "'/tmp/My Photos/photo.png' '/tmp/$(touch hacked).png' /tmp/plain.png '/tmp/it'\\''s.png'"
	if !ok || got != want {
		t.Fatalf("bash = %q", got)
	}
	if _, ok := formatClipboardFilePaths([]string{"/tmp/photo\x1b]0;unsafe\x07.png", "/tmp/ok.png"}, false); ok {
		t.Fatal("control characters should reject the whole paste")
	}
	if _, ok := formatClipboardFilePaths([]string{"/tmp/a\nb"}, true); ok {
		t.Fatal("newline in a path should reject the whole paste")
	}
}
