package tui

import (
	"runtime"
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
	if len(cmds) != 1 || !strings.Contains(cmds[0], "NSPasteboardURLReadingFileURLsOnlyKey") || !strings.Contains(cmds[0], "NSJSONSerialization") {
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
