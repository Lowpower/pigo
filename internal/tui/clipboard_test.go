package tui

import (
	"strings"
	"testing"
	"time"
)

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
