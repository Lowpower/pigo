package tui

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWriteClipboardTextWSLUsesWindowsClipboard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("linux WSL path")
	}
	var names []string
	var script string
	run := func(_ time.Duration, stdin string, name string, args ...string) ([]byte, bool) {
		names = append(names, name)
		if name == "wslpath" {
			if stdin != "" {
				t.Fatal("wslpath should not take stdin")
			}
			return []byte("C:\\Users\\x\\pigo-clip.txt\n"), true
		}
		if name == "powershell.exe" {
			script = strings.Join(args, " ")
			return []byte{}, true
		}
		t.Fatalf("unexpected command %s %v", name, args)
		return nil, false
	}
	getenv := func(k string) string {
		switch k {
		case "WSL_DISTRO_NAME":
			return "Ubuntu"
		default:
			return ""
		}
	}
	if !writeClipboardTextEnv(getenv, run, "hello 你好") {
		t.Fatal("expected windows clipboard write")
	}
	if len(names) < 2 || names[0] != "wslpath" || names[1] != "powershell.exe" {
		t.Fatalf("commands = %v", names)
	}
	if !strings.Contains(script, "Set-Clipboard") || !strings.Contains(script, "UTF8") {
		t.Fatalf("script = %q", script)
	}
	if !strings.Contains(script, `C:\Users\x\pigo-clip.txt`) {
		t.Fatalf("missing win path in %q", script)
	}
}

func TestCopyToClipboardIncludesOSC52(t *testing.T) {
	old := runClipCmd
	runClipCmd = func(time.Duration, string, string, ...string) ([]byte, bool) { return nil, false }
	t.Cleanup(func() { runClipCmd = old })
	got := copyToClipboard("abc")
	if !strings.Contains(got, "\x1b]52;c;") {
		t.Fatalf("osc = %q", got)
	}
}

