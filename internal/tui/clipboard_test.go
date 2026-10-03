package tui

import (
	"strings"
	"testing"
	"time"
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
